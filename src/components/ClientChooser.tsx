import { useState } from "react";
import { extraClients, type ExtraClient } from "../content/clients";
import { Modal } from "./Modal";

export function ClientChooser({
  current,
  disabled,
  onSelect,
}: {
  current: ExtraClient | null;
  disabled: boolean;
  onSelect: (id: ExtraClient) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const matches = extraClients.filter((c) =>
    `${c.name} ${c.id}`.toLowerCase().includes(search.trim().toLowerCase()),
  );
  return (
    <>
      <button
        className="button secondary client-chooser-button"
        disabled={disabled}
        onClick={() => {
          setSearch("");
          setOpen(true);
        }}
      >
        全部客户端 · {extraClients.length}
      </button>
      {open && (
        <Modal
          title="选择客户端"
          description="搜索客户端，选择后配置已保存的供应商。"
          onClose={() => setOpen(false)}
        >
          <label htmlFor="client-search">搜索客户端</label>
          <input
            id="client-search"
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <p role="status">{matches.length} 个客户端</p>
          <div className="client-chooser-list" aria-label="客户端列表">
            {matches.map((c) => (
              <button
                key={c.id}
                className="button secondary"
                aria-pressed={current === c.id}
                onClick={() => {
                  onSelect(c.id);
                  setOpen(false);
                }}
              >
                {c.name}
              </button>
            ))}
          </div>
        </Modal>
      )}
    </>
  );
}
