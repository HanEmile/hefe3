(format t "==> Loading vendored bundles~%")
(load "./bundle/bundle.lisp")

(format t "==> Adding the current dir to the asdf registry~%")
(push (uiop/os:getcwd) asdf:*central-registry*)

(format t "==> Loading vokodeep system~%")
(asdf:load-system :vokodeep :verbose t)

(require :sb-sprof)

(format t "==> Creating vokodeep executable~%")
(sb-ext:save-lisp-and-die "./vokodeep" :toplevel #'vokodeep::main :executable t)
