/**
 * Copyright (c) 2026 Adam Tait
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

// Test cases for no-raw-fetch-in-domain. Opengrep reads the annotation on the line
// before each match: `ruleid:` must match, `ok:` must not.

export async function loadOrder(id: string): Promise<unknown> {
  // ruleid: no-raw-fetch-in-domain
  const response = await fetch(`/orders/${id}`);
  return response.json();
}

interface Client {
  get(path: string): Promise<unknown>;
}

export async function loadOrderProperly(client: Client, id: string): Promise<unknown> {
  // ok: no-raw-fetch-in-domain
  return client.get(`/orders/${id}`);
}
