package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Job struct {
	ID             string `json:"id"`
	Description    string `json:"description"`
	Duration       int    `json:"duration"`
	Ratio          string `json:"ratio"`
	Resolution     string `json:"resolution"`
	Status         string `json:"status"`
	Provider       string `json:"provider"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	Error          string `json:"error"`
	ImageURL       string `json:"image_url"`
	VideoURL       string `json:"video_url"`
	ImagePath      string `json:"-"`
	ImageMIME      string `json:"-"`
	VideoPath      string `json:"-"`
	RemoteID       string `json:"-"`
	RemoteURL      string `json:"-"`
	IdempotencyKey string `json:"-"`
	RequestHash    string `json:"-"`
}

type Store struct{ DB *sql.DB }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "pic2video.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS jobs (
 id TEXT PRIMARY KEY, description TEXT NOT NULL, duration INTEGER NOT NULL, ratio TEXT NOT NULL,
 resolution TEXT NOT NULL, status TEXT NOT NULL, provider TEXT NOT NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, error TEXT NOT NULL DEFAULT '',
 image_path TEXT NOT NULL, image_mime TEXT NOT NULL, video_path TEXT NOT NULL DEFAULT '',
 remote_id TEXT NOT NULL DEFAULT '', remote_url TEXT NOT NULL DEFAULT '',
 idempotency_key TEXT NOT NULL UNIQUE, request_hash TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS jobs_status ON jobs(status);
CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, expires_at INTEGER NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

const columns = `id,description,duration,ratio,resolution,status,provider,created_at,updated_at,error,image_path,image_mime,video_path,remote_id,remote_url,idempotency_key,request_hash`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.Description, &j.Duration, &j.Ratio, &j.Resolution, &j.Status, &j.Provider, &j.CreatedAt, &j.UpdatedAt, &j.Error, &j.ImagePath, &j.ImageMIME, &j.VideoPath, &j.RemoteID, &j.RemoteURL, &j.IdempotencyKey, &j.RequestHash)
	if err == nil {
		j.ImageURL = "/api/jobs/" + j.ID + "/image"
		if j.Status == "succeeded" {
			j.VideoURL = "/api/jobs/" + j.ID + "/video"
		}
	}
	return j, err
}

func (s *Store) Create(j Job) error {
	_, err := s.DB.Exec(`INSERT INTO jobs (`+columns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.Description, j.Duration, j.Ratio, j.Resolution, j.Status, j.Provider, j.CreatedAt, j.UpdatedAt, j.Error, j.ImagePath, j.ImageMIME, j.VideoPath, j.RemoteID, j.RemoteURL, j.IdempotencyKey, j.RequestHash)
	return err
}
func (s *Store) Get(id string) (Job, error) {
	return scan(s.DB.QueryRow(`SELECT `+columns+` FROM jobs WHERE id=?`, id))
}
func (s *Store) ByKey(key string) (Job, error) {
	return scan(s.DB.QueryRow(`SELECT `+columns+` FROM jobs WHERE idempotency_key=?`, key))
}
func (s *Store) List(page, size int) ([]Job, int, error) {
	var total int
	if err := s.DB.QueryRow(`SELECT count(*) FROM jobs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(`SELECT `+columns+` FROM jobs ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		j, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, j)
	}
	return jobs, total, rows.Err()
}
func (s *Store) Next(provider string) (Job, error) {
	return scan(s.DB.QueryRow(`SELECT `+columns+` FROM jobs WHERE provider=? AND status IN ('queued','running','downloading') ORDER BY created_at,id LIMIT 1`, provider))
}
func (s *Store) Save(j *Job) error {
	j.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.DB.Exec(`UPDATE jobs SET status=?,updated_at=?,error=?,remote_id=?,remote_url=?,video_path=? WHERE id=?`, j.Status, j.UpdatedAt, j.Error, j.RemoteID, j.RemoteURL, j.VideoPath, j.ID)
	return err
}
func (s *Store) Recover(provider string) error {
	_, err := s.DB.Exec(`UPDATE jobs SET status='unknown',error='服务在提交期间中断，无法确认是否已产生收费任务。请先到火山引擎控制台核实，勿重复提交。',updated_at=? WHERE provider=? AND status='submitting'`, time.Now().UTC().Format(time.RFC3339Nano), provider)
	return err
}
func (s *Store) Delete(id string) error {
	_, err := s.DB.Exec(`DELETE FROM jobs WHERE id=?`, id)
	return err
}
func (s *Store) AddSession(hash string, expires time.Time) error {
	_, err := s.DB.Exec(`INSERT INTO sessions (token_hash,expires_at) VALUES (?,?)`, hash, expires.Unix())
	return err
}
func (s *Store) HasSession(hash string) bool {
	var expires int64
	err := s.DB.QueryRow(`SELECT expires_at FROM sessions WHERE token_hash=?`, hash).Scan(&expires)
	return err == nil && expires > time.Now().Unix()
}
func (s *Store) DeleteSession(hash string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token_hash=? OR expires_at<=?`, hash, time.Now().Unix())
	return err
}
func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
