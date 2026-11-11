try {
  var t = localStorage.getItem("homepanel-theme")
  if (t === "light" || (t === "system" && window.matchMedia("(prefers-color-scheme: light)").matches)) {
    document.documentElement.classList.remove("dark")
  }
} catch (e) {}
