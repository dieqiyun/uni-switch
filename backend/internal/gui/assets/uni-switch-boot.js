// Product presentation only. The app and every plugin are local source assets.
if (window.bootPrefs?.uniSwitch) {
  try {
    localStorage.setItem("uni-switch.navLayout", "side");
    if (!localStorage.getItem("uni-switch.lastView")) localStorage.setItem("uni-switch.lastView", "providers");
  } catch { /* this window still works without persisted layout */ }
  document.documentElement.classList.add("uni-switch");
}
