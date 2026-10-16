// 判题语言与代码模板：提交弹窗 / 在线编程共用。
// v0.4.7 个人设置落地后，默认语言与模板改为用户偏好覆盖。
export const languageOptions = [
  { value: 'cpp', label: 'C++ 14' },
  { value: 'c', label: 'C' },
  { value: 'python3', label: 'Python 3' },
]

export const codeTemplates: Record<string, string> = {
  cpp: '#include <iostream>\n\nint main() {\n    std::ios::sync_with_stdio(false);\n    int a, b;\n    std::cin >> a >> b;\n    std::cout << a + b << std::endl;\n    return 0;\n}\n',
  c: '#include <stdio.h>\n\nint main(void) {\n    int a, b;\n    scanf("%d %d", &a, &b);\n    printf("%d\\n", a + b);\n    return 0;\n}\n',
  python3: 'a, b = map(int, input().split())\nprint(a + b)\n',
}

export function templateFor(lang: string): string {
  return codeTemplates[lang] ?? ''
}
