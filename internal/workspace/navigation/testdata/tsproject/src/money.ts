// A name that is both a type and a value: an interface, and an object of helpers.
export interface Money {
  cents: number;
  currency: string;
}

export const Money = {
  zero(currency: string): Money {
    return { cents: 0, currency };
  },
};

export function add(a: Money, b: Money): Money {
  return { cents: a.cents + b.cents, currency: a.currency };
}
