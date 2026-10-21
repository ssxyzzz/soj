// 猜数交互器（v0.9.2 示例）
// 协议：从 argv[1] 读秘密数 N；反复从 stdin 读选手猜的数 x（stdout 回 "big"/"small"），
// 猜中回 "ok" 并退出码 0。最多 30 次机会，用尽退出码 1。
#include <cstdio>
#include <cstdlib>

int main(int argc, char** argv) {
    if (argc < 2) {
        fprintf(stderr, "interactor: missing input file\n");
        return 1;
    }
    FILE* f = fopen(argv[1], "r");
    if (!f) {
        fprintf(stderr, "interactor: cannot open %s\n", argv[1]);
        return 1;
    }
    long long secret = 0;
    if (fscanf(f, "%lld", &secret) != 1) {
        fclose(f);
        fprintf(stderr, "interactor: bad input\n");
        return 1;
    }
    fclose(f);

    for (int i = 0; i < 30; i++) {
        long long x = 0;
        if (scanf("%lld", &x) != 1) {
            printf("ok\n");
            fflush(stdout);
            fprintf(stderr, "选手输出格式错误\n");
            return 1;
        }
        if (x == secret) {
            printf("ok\n");
            fflush(stdout);
            fprintf(stderr, "猜中！共 %d 次\n", i + 1);
            return 0;
        }
        printf("%s\n", x > secret ? "big" : "small");
        fflush(stdout);
    }
    fprintf(stderr, "30 次机会用尽，未猜中\n");
    return 1;
}
