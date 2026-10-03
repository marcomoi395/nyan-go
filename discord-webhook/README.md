# Discord webhook web sender

Set `DISCORD_WEB_PASSWORD` and `DISCORD_WEBHOOK_URL` in the project `.env` file, then run:

```sh
docker compose --profile web up -d --build discord-webhook
```

Open <http://127.0.0.1:8080>. Set `DISCORD_WEB_PORT` to change the host port. For local development, use `docker compose -f compose.dev.yaml --profile web up discord-webhook`.

Enter the configured password to open the posting form. Reloading the page requires login again. Use **Chọn tệp +** to attach a file; **Bỏ tệp** clears the selection.
