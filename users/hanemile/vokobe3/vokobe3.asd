(asdf:defsystem "vokobe3"
  :description "a simple yet fun static site generator"
  :version "0.0.1"
  :author "Emile Hansmaennel <lisp@emile.space>"
  :license "Public Domain"
  :depends-on (#:cl-ppcre #:uiop)
  :serial t
  :components
    ((:file "packages")
     (:file "vokobe3")))
