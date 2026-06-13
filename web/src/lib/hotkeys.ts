import { Store } from "@tanstack/store";
import { useEffect } from "react";

export interface HotkeyHelp {
	label: string;
	group: string;
}

export interface HotkeyBinding {
	/** "j", "?", "enter", "escape", "arrowdown", or "mod+k" (mod = ⌘ on mac, ctrl elsewhere). */
	key: string;
	handler: (event: KeyboardEvent) => void;
	help?: HotkeyHelp;
	/** Fire even while an input/dialog has focus (default false; mod combos always fire). */
	force?: boolean;
}

export interface HotkeyHelpEntry extends HotkeyHelp {
	key: string;
}

/** Live list of registered bindings with help metadata — renders the ? overlay and feeds ⌘K. */
export const hotkeysRegistry = new Store<HotkeyHelpEntry[]>([]);

function isTypingTarget(target: EventTarget | null): boolean {
	if (!(target instanceof HTMLElement)) return false;
	if (target.isContentEditable) return true;
	const tag = target.tagName;
	return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}

function isDialogOpen(): boolean {
	return (
		document.querySelector(
			'[role="dialog"][data-state="open"], [role="alertdialog"][data-state="open"]',
		) !== null
	);
}

function matches(binding: HotkeyBinding, event: KeyboardEvent): boolean {
	const parts = binding.key.toLowerCase().split("+");
	const key = parts.at(-1) ?? "";
	const wantsMod = parts.includes("mod");
	const wantsShift = parts.includes("shift");
	if (event.key.toLowerCase() !== key) return false;
	const hasMod = event.metaKey || event.ctrlKey;
	if (wantsMod !== hasMod) return false;
	if (wantsShift !== event.shiftKey && key.length > 1) return false;
	return true;
}

export function useAppHotkeys(bindings: HotkeyBinding[], enabled = true): void {
	useEffect(() => {
		if (!enabled) return;

		const onKeyDown = (event: KeyboardEvent) => {
			for (const binding of bindings) {
				if (!matches(binding, event)) continue;
				const isModCombo = binding.key.includes("mod+");
				if (
					!isModCombo &&
					!binding.force &&
					(isTypingTarget(event.target) || isDialogOpen())
				) {
					continue;
				}
				event.preventDefault();
				binding.handler(event);
				return;
			}
		};

		const helpEntries: HotkeyHelpEntry[] = bindings
			.filter((b) => b.help)
			.map((b) => ({ key: b.key, ...(b.help as HotkeyHelp) }));
		hotkeysRegistry.setState((prev) => [...prev, ...helpEntries]);
		window.addEventListener("keydown", onKeyDown);

		return () => {
			window.removeEventListener("keydown", onKeyDown);
			hotkeysRegistry.setState((prev) =>
				prev.filter((e) => !helpEntries.includes(e)),
			);
		};
	});
}
