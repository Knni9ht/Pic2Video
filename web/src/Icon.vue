<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 20 })
const paths: Record<string, string> = {
  spark: 'm12 3 2.4 6.6L21 12l-6.6 2.4L12 21l-2.4-6.6L3 12l6.6-2.4L12 3Z',
  upload: 'M12 16V4m-4 4 4-4 4 4M4 16v4h16v-4',
  image: 'M4 4h16v16H4V4Zm0 12 5-5 5 5 3-3 3 3M14 8h.01',
  arrow: 'M4 12h16m-6-6 6 6-6 6',
  play: 'm9 5 11 7-11 7V5Z',
  check: 'm5 12 4 4L19 6',
  close: 'm6 6 12 12M18 6 6 18',
  clock: 'M12 8v4l3 2M22 12A10 10 0 1 1 2 12a10 10 0 0 1 20 0Z',
  download: 'M12 3v12m-4-4 4 4 4-4M4 16v5h16v-5',
  trash: 'M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7',
  refresh: 'M20 7a9 9 0 1 0 1 9M20 3v5h-5',
  logout: 'M9 4H4v16h5M9 12h12m-5-5 5 5-5 5',
  lock: 'M6 10h12v11H6V10Zm2 0V6a4 4 0 0 1 8 0v4',
  info: 'M12 11v6m0-10h.01M22 12A10 10 0 1 1 2 12a10 10 0 0 1 20 0Z',
  film: 'M4 3h16v18H4V3Zm0 5h16M4 16h16M8 3v5m8-5v5M8 16v5m8-5v5',
  chevron: 'm9 5 7 7-7 7',
}
</script>

<template>
  <svg :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="paths[name] || paths.spark" /></svg>
</template>
