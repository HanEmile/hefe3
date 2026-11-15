(format t "==> Loading vendored bundle~%")
;; If using a local bundle (Nix style)
(when (probe-file "./bundle/bundle.lisp")
  (load "./bundle/bundle.lisp"))

(format t "==> Loading vokobe3 system~%")
(push (uiop:getcwd) asdf:*central-registry*)
(asdf:load-system :vokobe3 :verbose t)

(format t "==> Creating vokobe3 executable~%")
;; We use the keyword :vokobe3 to ensure we reference the correct package
(sb-ext:save-lisp-and-die "./vokobe3"
                          :toplevel (symbol-function (find-symbol "MAIN" :vokobe3))
                          :executable t)
