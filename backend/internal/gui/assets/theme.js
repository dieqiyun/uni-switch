// Apply the saved theme and text size (boot.js) before styles first apply.
// A theme requested by the page's address takes precedence. Waiting for
// app.js at the foot of the page let the header paint in the system theme
// and default size first, then change as the page loaded.
(function () {
  var asked = new URLSearchParams(location.search).get("theme");
  var prefs = window.bootPrefs;
  var saved = prefs && prefs.theme;
  var theme = asked || (saved && saved !== "system" ? saved : "");
  if (theme) document.documentElement.dataset.theme = theme;
  if (prefs && prefs.colorblind) document.documentElement.dataset.colorblind = "1";
  if (prefs && prefs.reduceMotion) document.documentElement.dataset.motion = "reduce";
  if (prefs && prefs.noBird) document.documentElement.dataset.bird = "off";
  if (prefs) document.documentElement.style.setProperty("--zoom", prefs.web ? 1 : (prefs.textSize || 100) / 100);
})();

// Settings' Reduce motion (#3813) applies the stylesheets' reduced-motion
// rules, their media set to all and back; calm() in app.js follows it
(function () {
  var held = [];
  window.holdStill = function (on) {
    var root = document.documentElement;
    if (on) root.dataset.motion = "reduce"; else delete root.dataset.motion;
    held.forEach(function (h) { h[0].media.mediaText = h[1]; });
    held = [];
    if (on) for (var i = 0, s = document.styleSheets; i < s.length; i++) for (var j = 0, r = s[i].cssRules; j < r.length; j++)
      if (r[j].media && /reduced-motion: *reduce/.test(r[j].media.mediaText)) held.push([r[j], r[j].media.mediaText]), r[j].media.mediaText = "all";
  };
})();
