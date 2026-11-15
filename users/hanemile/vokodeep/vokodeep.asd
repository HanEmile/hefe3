(asdf:defsystem "vokodeep"
  :description "a simple yet fun static site generator"
  :version "0.0.1"
  :author "Emile Hansmaennel <lisp@emile.space>"
  :license "Public Domain"
  :depends-on (#:alexandria
               #:split-sequence
               #:cl-ppcre
               #:bordeaux-threads)
  :serial t
  :components
    ((:file "packages")
     (:file "vokodeep")))
