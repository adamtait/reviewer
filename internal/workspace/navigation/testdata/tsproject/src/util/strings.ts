export function pad(text: string, width: number): string {
  return text.padStart(width, ' ');
}

// Not ASCII on purpose: positions after it count UTF-16 units, not bytes.
export const greeting = '𝓗ello'; export const after = pad(greeting, 9);
