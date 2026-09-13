export type Order = {
  id: string;
  total: number;
};

export function totalWithTax(order: Order, rate: number): number {
  return order.total * (1 + rate);
}
