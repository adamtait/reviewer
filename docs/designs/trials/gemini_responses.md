# Responses observed from the Gemini API, 2026-11-23

Recorded while running [`gemini_live.py`](gemini_live.py) with a free-tier key. They are kept so the provider adapter's tests can replay real responses. Each body is as received. No key, repository or user name appears in any of them.

All but two came from the current conversation call, `POST /v1beta/interactions`, used with `store` false. The two from the older `generateContent` call are marked.

## What a streamed tool call looks like

The step types and delta types of one streamed response that asks for a tool:

```text
event: step.start    step.type = thought
event: step.delta    delta.type = thought_signature     (about 300 to 1,500 characters)
event: step.stop
event: step.start    step.type = function_call          (carries id, name and empty arguments)
event: step.delta    delta.type = arguments_delta       (JSON text, to be joined)
event: step.stop
event: interaction.completed    status = requires_action, with usage
```

A response that answers has a `thought` step and then a `model_output` step whose deltas have type `text`, and ends with status `completed`.

Usage fields read: `total_input_tokens`, `total_output_tokens`, `total_thought_tokens`.

## A rate limit: five requests a minute

HTTP 429.

```json
{"error":{"message":"Rate limit exceeded for model gemini-3-flash (limit: 5 requests per minute on Free Tier). Please retry in 36s or upgrade your tier at https://ai.dev/rate-limit.","code":"too_many_requests"}}
```

The response headers were not captured for this one. The wait is in the message.

## An exhausted daily quota: twenty requests a day

HTTP 429, with the header `retry-after: 31174`.

```json
{"error":{"message":"Rate limit exceeded for model gemini-3-flash (limit: 20 requests per day on Free Tier). Please retry in 8h39m34s or upgrade your tier at https://ai.dev/rate-limit.","code":"too_many_requests"}}
```

The status and code are the same as for the per-minute limit. Only the length of the wait differs.

## A model under heavy demand

HTTP 503. Seen on three models.

```json
{"error":{"message":"gemini-3.8-flash is currently experiencing high demand, spikes in demand are usually temporary. Please try again later.","code":"service_unavailable"}}
```

The older call answers the same condition in a different shape:

```json
{
  "error": {
    "code": 503,
    "message": "This model is currently experiencing high demand. Spikes in demand are usually temporary. Please try again later.",
    "status": "UNAVAILABLE"
  }
}
```

## Reasoning data left out of a tool loop

HTTP 400. The body arrives as a stream event.

```text
event: error
data: {"error":{"message":"Request contains an invalid argument.","code":"invalid_request"},"event_type":"error"}
```

The older call says more:

```json
{
  "error": {
    "code": 400,
    "message": "Function call is missing a thought_signature in functionCall parts. This is required for tools to work correctly, and missing thought_signature may lead to degraded model performance. Additional data, function call `default_api:search_text` , position 2. Please refer to https://aistudio.google.com/docs/thought-signatures for more details.",
    "status": "INVALID_ARGUMENT"
  }
}
```

## A key that is not valid

HTTP 401. Note the shape: a numeric code and a status, unlike the other errors from this call.

```json
{
  "error": {
    "code": 401,
    "message": "API key not valid. Please pass a valid API key.",
    "status": "UNAUTHENTICATED"
  }
}
```

The body also carried a `details` list, left out here.

## A model name that does not exist

HTTP 404.

```json
{"error":{"message":"Model 'gemini-does-not-exist' not found. Did you mean 'gemini-pro-latest'? Please verify the model name against the supported list: https://ai.google.dev/gemini-api/docs/models","code":"not_found"}}
```

## A follow-up question with the earlier turn as plain text

HTTP 200. The earlier question and answer were sent as a `user_input` step and a `model_output` step holding text, with the earlier `thought` step left out. The model answered correctly. Reasoning data is therefore needed only inside one question's tool loop.
