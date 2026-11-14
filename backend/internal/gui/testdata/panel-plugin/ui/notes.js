// A panel in each provider's editor: uni-switch.context.provider says which
// provider it is in, and the note is kept in the plugin's own options.
(async () => {
  await uniSwitch.ready;
  const id = uniSwitch.context.provider.id;
  let opts = await uniSwitch.options.get();
  const box = document.getElementById("note");
  box.value = opts.notes?.[id] || "";
  let timer = 0;
  // options are set once a second at most: the last keystroke's, a
  // little after it
  box.oninput = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      opts = await uniSwitch.options.set({ ...opts, notes: { ...opts.notes, [id]: box.value } });
      document.getElementById("saved").textContent = "Saved";
    }, 1200);
  };
})();
