export function total(amounts: number[]): number {
  return amounts.reduce((sum, amount) => sum + amount, 0);
}
