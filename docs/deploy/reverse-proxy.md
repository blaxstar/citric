# Behind a reverse proxy

Set `CITRIC_PUBLIC_URL` to the public URL so clone URLs and installers point
at it. Two things matter for the proxy itself:

- **Build logs stream** as server-sent events: disable response buffering.
- **Pushes can be large:** don't cap request bodies (or cap them generously).

## Caddy

```caddyfile
citric.example.com {
	reverse_proxy 127.0.0.1:4400 {
		flush_interval -1
	}
}
```

## nginx

```nginx
server {
    listen 443 ssl http2;
    server_name citric.example.com;
    # ssl_certificate ...;

    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:4400;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_read_timeout 1h;
    }
}
```

`/internal/hook/post-receive` is only called by citric's own git hooks and is
authenticated with a per-server token, so it needs no special proxy rule.
