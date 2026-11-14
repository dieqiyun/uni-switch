curl -X POST "https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/compatible-mode/v1/chat/completions" ^
-H "Authorization: Bearer %DASHSCOPE_API_KEY%" ^
-H "Content-Type: application/json" ^
-d "{
    \"model\": \"qwen3.8-max\",
    \"messages\": [
        {
            \"role\": \"system\",
            \"content\": \"You are a helpful assistant.\"
        },
        {
            \"role\": \"user\",
            \"content\": \"你是谁？\"
        }
    ]
}"
