import { Money, add } from './money';
import * as tax from './tax';
import { pad } from './util/strings';

export class Cart {
  private readonly items: Money[];

  constructor(items: Money[]) {
    this.items = items;
  }

  total(): Money {
    let sum = Money.zero('EUR');
    for (const item of this.items) {
      sum = add(sum, item);
    }
    return tax.withTax(sum);
  }

  label(): string {
    return pad(String(this.items.length), 3);
  }
}
