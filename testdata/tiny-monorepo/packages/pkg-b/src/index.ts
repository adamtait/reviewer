import { total } from '@tiny/pkg-a';

export function describe(amounts: number[]): string {
  return `total ${total(amounts)}`;
}
