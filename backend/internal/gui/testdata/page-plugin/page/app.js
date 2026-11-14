// What uni-switch's window gives a page: the theme, its own options, and the
// reads package.json's uni-switch.permissions declares.
(async () => {
  await uniSwitch.ready;
  const opts = await uniSwitch.options.get();
  const s = await uniSwitch.read("usage:summary", { period: "today" });
  document.getElementById("calls").textContent = `${s.calls} calls, ${s.input + s.output} tokens`;
  const cost = document.getElementById("cost");
  const paint = (hide) => { cost.textContent = hide ? "" : `$${s.cost.toFixed(2)}`; };
  paint(opts.hideCost);
  document.getElementById("hide").onclick = async () => {
    const next = await uniSwitch.options.set({ ...opts, hideCost: !opts.hideCost });
    Object.assign(opts, next);
    paint(opts.hideCost);
  };
})();
