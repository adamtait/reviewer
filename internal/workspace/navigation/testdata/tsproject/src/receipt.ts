import { format } from 'tiny-format';
import { Cart } from './cart';
import type { Money } from './money';

export function receipt(prices: Money[]): string {
  const cart = new Cart(prices);
  const total = cart.total();
  return `${cart.label()} ${format(total.cents)}`;
}
