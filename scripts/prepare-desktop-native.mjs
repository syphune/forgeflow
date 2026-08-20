import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { spawnSync } from "node:child_process";

if (process.platform === "darwin") {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const nodeGyp = path.join(root, "node_modules", ".bin", "node-gyp");
  for (const [packageName, output] of [
    ["macos-alias", "build/Release/volume.node"],
    ["fs-xattr", "build/Release/xattr.node"],
  ]) {
    const packageDir = path.join(root, "node_modules", packageName);
    if (existsSync(path.join(packageDir, output))) continue;
    if (!existsSync(nodeGyp)) {
      throw new Error(`node-gyp is required to package the macOS desktop app (${packageName})`);
    }
    const result = spawnSync(nodeGyp, ["rebuild"], {
      cwd: packageDir,
      stdio: "inherit",
    });
    if (result.status !== 0) {
      throw new Error(`could not build ${packageName}`);
    }
  }
}
