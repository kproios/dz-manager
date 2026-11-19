/* Rehearsal only: compile OFF the application host. Never publish as a release. */
#include <arpa/inet.h>
#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
#ifndef VERSION
#define VERSION "v0.0.100"
#endif
#ifndef BROKEN
#define BROKEN 0
#endif
int main(int argc, char **argv) {
    if (argc == 2 && !strcmp(argv[1], "version")) { puts(VERSION); return 0; }
    if (argc == 2 && !strcmp(argv[1], "check-config")) return getenv("DZM_SECRET_KEY") ? 0 : 1;
    if (argc != 2 || strcmp(argv[1], "serve")) return 1;
    if (BROKEN) {
        pid_t child = fork();
        if (!child) { execl("/usr/bin/sqlite3", "sqlite3", "/var/lib/dzm/dzm.db", "INSERT INTO rehearsal VALUES ('broken-migration');", (char *)0); _exit(1); }
        waitpid(child, NULL, 0);
    }
    signal(SIGPIPE, SIG_IGN);
    int s = socket(AF_INET, SOCK_STREAM, 0), yes = 1;
    setsockopt(s, SOL_SOCKET, SO_REUSEADDR, &yes, sizeof yes);
    struct sockaddr_in addr = {.sin_family=AF_INET, .sin_port=htons(8080)};
    inet_pton(AF_INET, "127.0.0.1", &addr.sin_addr);
    if (bind(s, (void *)&addr, sizeof addr) || listen(s, 16)) { perror("listen"); return 1; }
    for (;;) {
        int c = accept(s, NULL, NULL);
        if (c < 0) continue;
        struct timeval tv = {.tv_sec=3};
        setsockopt(c, SOL_SOCKET, SO_RCVTIMEO, &tv, sizeof tv);
        char request[4096]={0}, method[16]={0}, path[2048]={0}, body[4096], extra[2048]={0};
        if (recv(c, request, sizeof(request)-1, 0) <= 0) {close(c); continue;}
        sscanf(request, "%15s %2047s", method, path);
        int status=200;
        if (!strcmp(path, "/ready")) {
            status = BROKEN ? 503 : 200;
            snprintf(body, sizeof body, "{\"status\":\"%s\",\"version\":\"%s\",\"rehearsal_stub\":true}", BROKEN ? "not_ready" : "ready", VERSION);
        } else if (!strcmp(path, "/health") || !strcmp(path, "/ingest/ping")) {
            snprintf(body, sizeof body, "{\"rehearsal_stub\":true,\"version\":\"%s\"}", VERSION);
        } else {
            status=302;
            snprintf(body, sizeof body, "Rehearsal stub; application and login implementation pending.\n");
            const char *issuer=getenv("DZM_OIDC_ISSUER"), *client=getenv("DZM_OIDC_CLIENT_ID");
            snprintf(extra, sizeof extra, "Location: https://sso.example.com/application/o/authorize/?client_id=%s&response_type=code&scope=openid%%20email%%20profile&redirect_uri=https%%3A%%2F%%2Fdzm.example.com%%2Fauth%%2Fcallback\r\n", issuer && client ? client : "pending");
        }
        char response[8192];
        int len=snprintf(response, sizeof response, "HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nCache-Control: no-store\r\n%sContent-Length: %zu\r\nConnection: close\r\n\r\n%s", status, status==200 ? "OK" : status==302 ? "Found" : "Service Unavailable", extra, strlen(body), body);
        send(c, response, len, MSG_NOSIGNAL);
        close(c);
    }
}
