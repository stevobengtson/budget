import { useDebouncedValue } from "@tanstack/react-pacer";
import { SearchIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { Input } from "#/components/ui/input.tsx";

export function FilterBar({
	initialValue,
	onFilterChange,
}: {
	initialValue: string;
	/** Called with the debounced value; parent syncs it to ?filter= */
	onFilterChange: (value: string) => void;
}) {
	const [raw, setRaw] = useState(initialValue);
	const [debounced] = useDebouncedValue(raw, { wait: 250 });

	useEffect(() => {
		onFilterChange(debounced);
	}, [debounced, onFilterChange]);

	return (
		<div className="relative w-full max-w-sm">
			<SearchIcon className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
			<Input
				value={raw}
				onChange={(e) => setRaw(e.target.value)}
				placeholder="Filter by payee, category, memo…"
				className="pl-8"
				aria-label="Filter transactions"
			/>
		</div>
	);
}
