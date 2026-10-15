// Package lang 描述各判题语言在 go-judge 沙箱内的编译/运行方式。
package lang

// Language 中编译产物固定叫 main（copyOutCached），运行时通过 fileId 复用。
type Language struct {
	Name        string   // 展示名
	SourceFile  string   // 源文件名
	NeedCompile bool     // 是否需要编译
	CompileArgs []string // 编译命令：源码经 stdin 传入，输出 ./main
	RunArgs     []string // 运行命令
}

var Languages = map[string]*Language{
	"c": {
		Name:        "C (gcc)",
		SourceFile:  "main.c",
		NeedCompile: true,
		CompileArgs: []string{"/usr/bin/gcc", "-x", "c", "-", "-o", "main", "-O2", "-w", "-std=c11", "-pipe"},
		RunArgs:     []string{"./main"},
	},
	"cpp": {
		Name:        "C++ 14 (NOI)",
		SourceFile:  "main.cpp",
		NeedCompile: true,
		CompileArgs: []string{"/usr/bin/g++", "-x", "c++", "-", "-o", "main", "-O2", "-w", "-std=c++14", "-pipe"},
		RunArgs:     []string{"./main"},
	},
	"python3": {
		Name:       "Python 3",
		SourceFile: "main.py",
		RunArgs:    []string{"/usr/bin/python3", "main.py"},
	},
}

func Exists(name string) bool {
	_, ok := Languages[name]
	return ok
}
