import { send } from "../infra/http.js";

export type Order = {
  id: string;
  total: number;
};

export function totalWithTax(order: Order, rate: number): number {
  return order.total * (1 + rate);
}

export function submit(order: Order): void {
  send({ url: "/orders", body: JSON.stringify(order) });
}
