# Recorded provider responses

What Google's `interactions` call sent, kept so the adapter's tests replay real responses and not
ones written to match the adapter.

| File | What it is |
| --- | --- |
| `tool_call.request.json`, `tool_call.sse` | The first call of a conversation: a question, answered by a request to run a tool. |
| `second_tool_call.request.json`, `second_tool_call.sse` | The second call: the first tool's result sent back, with the provider's reasoning data, answered by a request for a second tool. |
| `answer.request.json`, `answer.sse` | The third call, answered with text. |
| `rate_limit.json` | HTTP 429. A per-minute limit. The wait is only in the message. |
| `quota.json` | HTTP 429. An exhausted daily quota. It arrived with the header `retry-after: 31174`. |
| `overloaded.json` | HTTP 503. |
| `overloaded_older_shape.json` | HTTP 503 in the other shape an error body can take: a numeric code and a status. |
| `missing_reasoning.sse` | HTTP 400. Reasoning data was left out of a tool loop. The body is a stream event. |
| `bad_key.json` | HTTP 401, in the numeric shape. |
| `unknown_model.json` | HTTP 404. |

The three streamed conversations were recorded on 2026-11-24 with
`RECORD_DIR=… docs/designs/trials/gemini_live.py new gemini-3-flash-preview`, which writes each
request and each response exactly as sent and received. The key is never written. The question is
about a two-file made-up repository that the script holds.

The error bodies were observed on 2026-11-23 and are also in
`docs/designs/trials/gemini_responses.md`, with the status and headers each arrived with.

The long `signature` values are the provider's reasoning data for this conversation. They are
opaque, and mean nothing outside it.
