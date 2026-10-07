import { useEffect, useRef, useState } from "react";
import { Check, Pencil, X } from "lucide-react";
import type { Provider } from "../types";

export function ProviderName({
  provider,
  displayName,
  disabled,
  editing,
  onEditing,
  onSave,
}: {
  provider: Provider;
  displayName: string;
  disabled: boolean;
  editing: boolean;
  onEditing: (value: boolean) => void;
  onSave: (provider: Provider, name: string) => Promise<boolean>;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [name, setName] = useState(provider.name);
  const original = useRef(provider);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const lock = useRef(false);
  useEffect(() => {
    if (editing) {
      original.current = provider;
      setName(provider.name);
      setError("");
      input.current?.focus();
      input.current?.select();
    }
  }, [editing]);
  async function save() {
    if (disabled || lock.current) return;
    const value = name.trim();
    if (!value || new TextEncoder().encode(value).length > 160) {
      setError("名称为空或过长，请调整后保存。");
      return;
    }
    if (value === original.current.name) {
      onEditing(false);
      return;
    }
    lock.current = true;
    setSaving(true);
    try {
      if (await onSave(original.current, value)) onEditing(false);
    } finally {
      lock.current = false;
      setSaving(false);
    }
  }
  return editing ? (
    <form
      className="provider-name-edit"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <input
        ref={input}
        aria-label={`供应商名称 · ${provider.name}`}
        value={name}
        disabled={disabled || saving}
        aria-invalid={!!error}
        aria-describedby={error ? `rename-error-${provider.id}` : undefined}
        onChange={(e) => {
          setName(e.target.value);
          setError("");
        }}
        onKeyDown={(e) => {
          if (e.key === "Escape" && !saving) {
            e.preventDefault();
            onEditing(false);
          }
        }}
      />
      <button
        type="submit"
        className="icon-button"
        disabled={disabled || saving}
        aria-label="保存供应商名称"
        title="保存名称"
      >
        <Check size={14} aria-hidden />
      </button>
      <button
        type="button"
        className="icon-button"
        disabled={disabled || saving}
        aria-label="取消名称修改"
        title="取消"
        onClick={() => onEditing(false)}
      >
        <X size={14} aria-hidden />
      </button>
      {error && (
        <p
          id={`rename-error-${provider.id}`}
          className="field-error"
          role="alert"
        >
          {error}
        </p>
      )}
    </form>
  ) : (
    <h3 title={provider.name}>
      <button
        type="button"
        className="provider-name-button"
        disabled={disabled}
        data-provider-name={provider.id}
        aria-label={`修改名称 ${provider.name}`}
        title="点击修改名称"
        onClick={() => onEditing(true)}
      >
        <span>{displayName}</span>
        <Pencil size={12} aria-hidden />
      </button>
    </h3>
  );
}
