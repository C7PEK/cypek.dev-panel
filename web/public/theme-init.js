// Apply the stored theme before React mounts to avoid a flash. Dark is the
// default. A separate file so the Content-Security-Policy can forbid inline
// scripts.
try {
  var t = localStorage.getItem("homepanel-theme")
  if (t === "light" || (t === "system" && window.matchMedia("(prefers-color-scheme: light)").matches)) {
    document.documentElement.classList.remove("dark")
  }
} catch (e) {}
