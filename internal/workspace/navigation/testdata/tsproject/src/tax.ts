import type { Money } from './money';

export const rate = 0.2;

export function withTax(amount: Money): Money {
  return { cents: Math.round(amount.cents * (1 + rate)), currency: amount.currency };
}
