import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { hotkeysRegistry, useAppHotkeys } from "./hotkeys.ts";

function fire(key: string, init: KeyboardEventInit = {}, target?: HTMLElement) {
	const event = new KeyboardEvent("keydown", { key, bubbles: true, ...init });
	(target ?? document.body).dispatchEvent(event);
}

function Harness({ onJ, onMod }: { onJ: () => void; onMod?: () => void }) {
	useAppHotkeys([
		{ key: "j", handler: onJ, help: { label: "Down", group: "Navigation" } },
		...(onMod
			? [
					{
						key: "mod+k",
						handler: onMod,
						help: { label: "Palette", group: "Global" },
					},
				]
			: []),
	]);
	return <input data-testid="field" />;
}

describe("useAppHotkeys", () => {
	it("fires handler on plain key", () => {
		const onJ = vi.fn();
		render(<Harness onJ={onJ} />);
		fire("j");
		expect(onJ).toHaveBeenCalledOnce();
	});

	it("suppresses plain keys while typing in an input", () => {
		const onJ = vi.fn();
		const { getByTestId } = render(<Harness onJ={onJ} />);
		const input = getByTestId("field");
		input.focus();
		fire("j", {}, input);
		expect(onJ).not.toHaveBeenCalled();
	});

	it("fires mod+k even from an input", () => {
		const onJ = vi.fn();
		const onMod = vi.fn();
		const { getByTestId } = render(<Harness onJ={onJ} onMod={onMod} />);
		const input = getByTestId("field");
		input.focus();
		fire("k", { metaKey: true }, input);
		expect(onMod).toHaveBeenCalledOnce();
	});

	it("unregisters on unmount", () => {
		const onJ = vi.fn();
		const { unmount } = render(<Harness onJ={onJ} />);
		unmount();
		fire("j");
		expect(onJ).not.toHaveBeenCalled();
	});

	it("publishes help entries to the registry and removes them on unmount", () => {
		const { unmount } = render(<Harness onJ={() => {}} />);
		const entries = hotkeysRegistry.state;
		expect(entries.some((e) => e.label === "Down" && e.key === "j")).toBe(true);
		unmount();
		expect(hotkeysRegistry.state.some((e) => e.label === "Down")).toBe(false);
	});
});
