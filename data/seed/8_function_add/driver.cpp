int add(int a, int b);

int main() {
    int a, b;
    if (scanf("%d%d", &a, &b) != 2) return 1;
    printf("%d\n", add(a, b));
    return 0;
}
