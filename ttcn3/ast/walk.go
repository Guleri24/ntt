package ast

// WalkModuleDefs calls fun for every module definition.
func WalkModuleDefs(fun func(def *ModuleDef) bool, nodes ...Node) {
	for _, n := range nodes {
		switch x := n.(type) {
		case *Module:
			walkModuleDefs(fun, x.Defs...)
		case *GroupDecl:
			walkModuleDefs(fun, x.Defs...)
		case *ModuleDef:
			if g, ok := x.Def.(*GroupDecl); ok {
				WalkModuleDefs(fun, g)
				return
			}
			if !fun(x) {
				return
			}
		}
	}
}

func walkModuleDefs(fun func(def *ModuleDef) bool, defs ...*ModuleDef) {
	for _, d := range defs {
		WalkModuleDefs(fun, d)
	}
}
