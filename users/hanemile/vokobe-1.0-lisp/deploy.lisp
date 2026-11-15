;(load "deploy-common.lisp")

(format t "==> Loading vendored bundle~%")
(load "./bundle/bundle.lisp")
(format t "==> Loading vokobe2 system~%")
;; Register the .asd file inside this directory with ASDF.
(push (uiop/os:getcwd) asdf:*central-registry*)
(asdf:load-system :vokobe2 :verbose t)
;(format t "==> Loaded models:~%")
;(loop
;  for x being the hash-values of trackernet::*line-models*
;  do (format t "    ~A~%" x))

(require :sb-sprof)
(format t "==> Creating vokobe2 executable~%")
(sb-ext:save-lisp-and-die "./vokobe2" :toplevel #'vokobe2::main :executable t)
