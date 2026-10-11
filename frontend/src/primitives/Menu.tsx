import { createPortal } from "preact/compat";
import { useLayoutEffect, useRef } from "preact/hooks";
import { s } from "../styles";

export type MenuAction = { label: string; onSelect: () => void; disabled?: boolean };
export function Menu({
  x,
  y,
  label,
  items,
  onClose,
}: {
  x: number;
  y: number;
  label: string;
  items: MenuAction[];
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const root = ref.current;
    if (!root) return;
    const previous = document.activeElement;
    const bounds = root.getBoundingClientRect();
    root.style.left = `${Math.max(8, Math.min(x, innerWidth - bounds.width - 8))}px`;
    root.style.top = `${Math.max(8, Math.min(y, innerHeight - bounds.height - 8))}px`;
    root.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
    const outside = (event: PointerEvent) => {
      if (event.target instanceof Node && !root.contains(event.target)) onClose();
    };
    document.addEventListener("pointerdown", outside);
    window.addEventListener("resize", onClose);
    window.addEventListener("blur", onClose);
    window.addEventListener("scroll", onClose, true);
    return () => {
      document.removeEventListener("pointerdown", outside);
      window.removeEventListener("resize", onClose);
      window.removeEventListener("blur", onClose);
      window.removeEventListener("scroll", onClose, true);
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, [x, y]);
  return createPortal(
    <div
      ref={ref}
      role="menu"
      aria-label={label}
      class={s.contextMenu}
      style={{ left: x, top: y }}
      onContextMenu={(event) => event.preventDefault()}
      onKeyDown={(event) => {
        const buttons = [
          ...(ref.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") || []),
        ];
        const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
        if (["Escape", "Tab"].includes(event.key)) {
          event.preventDefault();
          onClose();
        } else if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
          event.preventDefault();
          const next =
            event.key === "Home"
              ? 0
              : event.key === "End"
                ? buttons.length - 1
                : (index + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
          buttons[next]?.focus();
        }
      }}
    >
      {items.map((item) => (
        <button
          key={item.label}
          type="button"
          role="menuitem"
          class={s.menuItem}
          disabled={item.disabled}
          onClick={() => {
            onClose();
            item.onSelect();
          }}
        >
          {item.label}
        </button>
      ))}
    </div>,
    document.body,
  );
}
