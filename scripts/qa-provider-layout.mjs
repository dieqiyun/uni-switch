import assert from "node:assert/strict";

export async function verifyProviderLayout({ page, checks }) {
  const layout = await page.locator(".provider-card").evaluateAll((rows) =>
    rows.map((row) => {
      const fields = [
        ...row.querySelectorAll(".provider-meta .provider-setting"),
      ];
      const name = row
        .querySelector(".provider-name-button")
        .getBoundingClientRect();
      const pin = row
        .querySelector(".provider-pin-button")
        .getBoundingClientRect();
      return {
        pinGap: pin.left - name.right,
        syncInActions: !!row.querySelector(
          ".provider-actions .provider-sync-button",
        ),
        fields: fields.map((field) => {
          const label = field.querySelector(".provider-setting-label");
          const value = field.querySelector(
            "code, .provider-context-input, .provider-fast-toggle, .balance-summary",
          );
          const rect = field.getBoundingClientRect();
          return {
            label: label.textContent.trim(),
            x: rect.x,
            width: rect.width,
            labelY: label.getBoundingClientRect().top,
            valueY: value.getBoundingClientRect().top,
          };
        }),
      };
    }),
  );
  assert.ok(layout.length >= 2);
  for (const entry of layout) {
    assert.equal(entry.fields.length, 4);
    assert.deepEqual(
      entry.fields.slice(0, 3).map((f) => f.label),
      ["默认模型", "上下文长度", "Fast 加速模式"],
    );
    assert.match(entry.fields[3].label, /余额|额度/);
    assert.ok(
      entry.pinGap >= 0 && entry.pinGap <= 18,
      "Pin stays beside supplier name",
    );
    assert.equal(
      entry.syncInActions,
      false,
      "Cross-client sync cannot shift primary actions",
    );
    for (const field of entry.fields) {
      assert.ok(
        Math.abs(field.labelY - entry.fields[0].labelY) < 1,
        "Field labels share a baseline",
      );
      assert.ok(
        Math.abs(field.valueY - entry.fields[0].valueY) < 1,
        "Field values share a baseline",
      );
    }
  }
  for (const entry of layout.slice(1)) {
    entry.fields.forEach((field, i) => {
      assert.ok(
        Math.abs(field.x - layout[0].fields[i].x) < 1,
        "Every supplier uses the same column start",
      );
      assert.ok(
        Math.abs(field.width - layout[0].fields[i].width) < 1,
        "Every supplier uses the same column width",
      );
    });
  }
  checks.push(
    "供应商四列标签、数值和列宽逐项对齐，置顶紧邻名称，跨客户端同步不挤占使用与编辑操作",
  );
  return layout;
}
