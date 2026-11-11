// A panel at the end of the Usage page: today's three most used models,
// from the usage:summary read its package declares.
(async () => {
  await uniSwitch.ready;
  const s = await uniSwitch.read("usage:summary", { period: "today" });
  const top = document.getElementById("top");
  for (const m of s.models.slice(0, 3)) {
    const li = document.createElement("li");
    li.textContent = `${m.model}: ${m.calls} calls`;
    top.append(li);
  }
  document.getElementById("none").hidden = s.models.length > 0;
})();
