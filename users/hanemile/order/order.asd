(defsystem :order
  :description "A lisp-powered personal wiki and knowledge management system"
  :version "0.1.0"
  :author "emile"
  :license "MIT"
  :serial t
  :components ((:file "src/package")
               (:file "src/util")
               (:file "src/reader")
               (:file "src/object")
               (:file "src/query")
               (:file "src/backlinks")
               (:file "src/render")
               (:file "src/topology")
               (:file "src/cache")
               (:file "src/site")
               (:file "src/main")))
