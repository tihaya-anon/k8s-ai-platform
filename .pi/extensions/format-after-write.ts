import path from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function (pi: ExtensionAPI) {
	pi.on("tool_result", async (event) => {
		if (event.isError) return;
		if (event.toolName !== "edit" && event.toolName !== "write") return;

		const input = event.input as { path?: unknown };
		if (typeof input.path !== "string") return;

		const root = process.cwd();
		const absolute = path.resolve(root, input.path);
		const relative = path.relative(root, absolute);

		// Prevent formatting paths outside the repository.
		if (
			!relative ||
			relative === ".." ||
			relative.startsWith(`..${path.sep}`) ||
			path.isAbsolute(relative)
		) {
			return;
		}

		const result = await pi.exec("./hack/format.sh", [relative]);

		if (result.code !== 0) {
			console.error(`Formatting failed for ${relative}\n${result.stderr ?? ""}`);
		}
	});
}
