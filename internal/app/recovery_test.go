package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Knni9ht/Pic2Video/internal/provider"
	"github.com/Knni9ht/Pic2Video/internal/store"
)

func completedFixture(t *testing.T, a *App) store.Job {
	t.Helper()
	h := a.Handler(nil)
	j := decodeJob(t, submit(t, h, loggedIn(t, h), "delete-fixture-123", "测试商品", pngImage(t, 300, 300)))
	j, err := a.Store.Get(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.Provider = &provider.Mock{Delay: time.Nanosecond}
	a.process(context.Background(), &j)
	completed, err := a.Store.Get(j.ID)
	if err != nil || completed.Status != "succeeded" {
		t.Fatalf("fixture: %+v %v", completed, err)
	}
	return completed
}

func TestDeletionRollbackWithLockedWindowsFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows file handles deny renaming open media files")
	}
	a, h := newTestApp(t)
	j := completedFixture(t, a)
	// Lock the second file so the first rename must be rolled back.
	locked, err := os.Open(filepath.Join(a.Config.DataDir, j.ImagePath))
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Close()
	cookie := loggedIn(t, h)
	if w := request(h, cookie, "DELETE", "/api/jobs/"+j.ID); w.Code != 500 {
		t.Fatalf("locked deletion: %d %s", w.Code, w.Body.String())
	}
	for _, relative := range []string{j.ImagePath, j.VideoPath} {
		if _, err := os.Stat(filepath.Join(a.Config.DataDir, relative)); err != nil {
			t.Fatalf("failed deletion lost %s: %v", relative, err)
		}
	}
	if _, err := a.Store.Get(j.ID); err != nil {
		t.Fatalf("failed deletion lost record: %v", err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}
	if w := request(h, cookie, "DELETE", "/api/jobs/"+j.ID); w.Code != 204 {
		t.Fatalf("retry deletion: %d %s", w.Code, w.Body.String())
	}
}

func TestRecoveryOfInterruptedDeletion(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "record-kept"
		if committed {
			name = "record-deleted"
		}
		t.Run(name, func(t *testing.T) {
			a, _ := newTestApp(t)
			j := completedFixture(t, a)
			for _, relative := range []string{j.ImagePath, j.VideoPath} {
				path := filepath.Join(a.Config.DataDir, relative)
				if err := os.Rename(path, path+".delete"); err != nil {
					t.Fatal(err)
				}
			}
			if committed {
				if err := a.Store.Delete(j.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.Store.Close(); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(a.Config, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Store.Close()
			for _, relative := range []string{j.ImagePath, j.VideoPath} {
				path := filepath.Join(a.Config.DataDir, relative)
				if _, err := os.Stat(path + ".delete"); !os.IsNotExist(err) {
					t.Fatalf("pending deletion remained: %v", err)
				}
				_, err := os.Stat(path)
				if committed && !os.IsNotExist(err) || !committed && err != nil {
					t.Fatalf("incorrect recovery (committed=%v): %v", committed, err)
				}
			}
		})
	}
}

func TestProviderSwitchPreservesRunningTask(t *testing.T) {
	a, h := newTestApp(t)
	j := decodeJob(t, submit(t, h, loggedIn(t, h), "provider-switch-123", "测试商品", pngImage(t, 300, 300)))
	j.Status, j.RemoteID = "running", "remote-id"
	if err := a.Store.Save(&j); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Close(); err != nil {
		t.Fatal(err)
	}
	switchedConfig := a.Config
	switchedConfig.Provider = "seedance"
	switched, err := New(switchedConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	switched.Provider = &controlledProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); switched.Run(ctx) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
	unchanged, err := switched.Store.Get(j.ID)
	if err != nil || unchanged.Status != "running" || unchanged.RemoteID != "remote-id" {
		t.Fatalf("other provider damaged pending task: %+v %v", unchanged, err)
	}
	if err := switched.Store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(a.Config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restarted.Store.Close() })
	p := &controlledProvider{}
	restarted.Provider = p
	startWorker(t, restarted)
	waitStatus(t, restarted, j.ID, "succeeded")
	if p.Submits.Load() != 0 {
		t.Fatal("provider switch resubmitted existing task")
	}
}
