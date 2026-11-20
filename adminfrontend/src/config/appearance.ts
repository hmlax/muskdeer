/** Restore the current application's theme before importing React and its stores. */
export function restoreApplicationTheme(storageKey: string): void {
  let mode = 'dark';
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) ?? 'null');
    const storedMode = saved?.state?.preferences?.theme?.mode;
    if (storedMode === 'light' || storedMode === 'dark' || storedMode === 'auto') {
      mode = storedMode;
    }
  } catch (error) {
    // Local preferences are best-effort; unavailable storage uses the default theme.
    console.warn('Failed to restore application theme:', error);
  }
  if (mode === 'auto') {
    mode = window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  }
  document.documentElement.setAttribute('data-theme', mode);
  document.documentElement.style.backgroundColor = mode === 'dark' ? '#0B0F19' : '#f5f5f5';
}
