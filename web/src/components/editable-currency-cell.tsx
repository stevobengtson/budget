import { useState } from "react";
import { Amount } from "#/components/amount.tsx";
import { Input } from "#/components/ui/input.tsx";
import { parseCents } from "#/lib/money.ts";
import { cn } from "#/lib/utils.ts";

export function EditableCurrencyCell({
	cents,
	onSave,
	className,
	ariaLabel,
}: {
	cents: number;
	onSave: (cents: number) => void;
	className?: string;
	ariaLabel: string;
}) {
	const [editing, setEditing] = useState(false);
	const [value, setValue] = useState("");
	const [invalid, setInvalid] = useState(false);

	function start() {
		setValue((cents / 100).toFixed(2));
		setInvalid(false);
		setEditing(true);
	}

	function commit() {
		const parsed = parseCents(value);
		if (parsed === null || parsed < 0) {
			setInvalid(true);
			return;
		}
		setEditing(false);
		if (parsed !== cents) onSave(parsed);
	}

	if (!editing) {
		return (
			<button
				type="button"
				aria-label={`Edit ${ariaLabel}`}
				onClick={start}
				className={cn(
					"cursor-text rounded px-1 text-right hover:bg-accent hover:outline hover:outline-border",
					className,
				)}
			>
				<Amount cents={cents} tone="neutral" />
			</button>
		);
	}

	return (
		<Input
			autoFocus
			value={value}
			onChange={(e) => {
				setValue(e.target.value);
				setInvalid(false);
			}}
			onBlur={commit}
			onKeyDown={(e) => {
				if (e.key === "Enter") commit();
				if (e.key === "Escape") setEditing(false);
			}}
			aria-label={ariaLabel}
			aria-invalid={invalid}
			className={cn(
				"h-7 w-28 text-right",
				invalid && "border-destructive",
				className,
			)}
		/>
	);
}
