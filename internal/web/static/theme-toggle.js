// Two-state theme toggle. Pre-paint init lives inline in <head> (layout.templ);
// this file only flips the class and persists the explicit choice.
function toggleTheme() {
  var dark = document.documentElement.classList.toggle("dark");
  localStorage.setItem("theme", dark ? "dark" : "light");
}
