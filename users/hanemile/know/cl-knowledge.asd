(in-package :asdf)

(defsystem :gambol
  :name "CL-KNOWLEDGE"
  :author "Emile Hansmaennel <lisp@emile.space>"
  :version "0.01"
  :maintainer "Emile Hansmaennel <lisp@emile.space>"
  :licence "MIT License"
  :description "A knowledge framework"

  :components ((:file "package")
               (:file "know""))
  :serial t)
