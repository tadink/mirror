server{
    listen 80 default_server;
    server_name www.domain.com;
    access_log logs/domain.com.access.log;
    error_log logs/domain.com.error.log;

    location / {
        proxy_pass http://127.0.0.1:8899;
        proxy_set_header Accept-Encoding "";
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For  $proxy_add_x_forwarded_for;
        proxy_set_header Host $host;
        proxy_cache off;
        proxy_set_header scheme $scheme;
 }
}