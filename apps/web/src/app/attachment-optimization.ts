const MAX_IMAGE_EDGE = 2048;
const WEBP_QUALITY = 0.82;

export type PreparedAttachment = {
  file: File;
  originalSize: number;
  optimized: boolean;
};

export async function optimizeAttachment(file: File): Promise<PreparedAttachment> {
  const original = { file, originalSize: file.size, optimized: false };
  if (!file.type.startsWith("image/") || file.type === "image/gif" || file.type === "image/svg+xml" || file.size <= 512 * 1024 || typeof globalThis.createImageBitmap !== "function" || typeof document === "undefined") return original;

  let bitmap: ImageBitmap | undefined;
  try {
    bitmap = await globalThis.createImageBitmap(file);
    const scale = Math.min(1, MAX_IMAGE_EDGE / bitmap.width, MAX_IMAGE_EDGE / bitmap.height);
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const context = canvas.getContext("2d");
    if (!context) return original;
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/webp", WEBP_QUALITY));
    if (!blob || blob.size >= file.size) return original;
    const name = file.name.replace(/\.[^.]+$/, "") || "attachment";
    return { file: new File([blob], `${name}.webp`, { type: "image/webp", lastModified: file.lastModified }), originalSize: file.size, optimized: true };
  } catch {
    return original;
  } finally {
    bitmap?.close();
  }
}
