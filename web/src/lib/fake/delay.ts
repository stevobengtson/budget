/** Small artificial latency so loading states render. Zero under vitest. */
export function fakeLatency(): Promise<void> {
	const ms = import.meta.env.MODE === "test" ? 0 : 150;
	return new Promise((resolve) => setTimeout(resolve, ms));
}
