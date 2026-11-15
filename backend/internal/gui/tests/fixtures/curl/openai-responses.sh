curl https://api.openai.com/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6-astra",
    "instructions": "You are a personal math tutor. When asked a math question, write and run code to answer the question.",
    "tools": [
      {
        "type": "code_interpreter",
        "container": { "type": "auto" }
      }
    ],
    "input": "I need to solve the equation 3x + 11 = 14. Can you help me?"
  }'
