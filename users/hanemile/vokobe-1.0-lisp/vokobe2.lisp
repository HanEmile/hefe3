(in-package :vokobe2)

; I tried using css-lite, yet it was aweful, as it didn't support media queries
; and overall feld kind of "not production ready" (cool, yet not what I was
; searching for
(defun style (filepath)
  "a list of lines containing the css"
  (with-open-file (stream filepath :direction :input)
    (loop for line = (read-line stream nil)
          while line
          collect line)))

(defmacro with-page ((&key title) &body body)
       `(with-html
          (:doctype)
          (:html
           (:head
            (:meta :charset :utf-8)
            (:meta :http-equiv "X-UA-Compatible"
                   :content "IE=edge")
            (:meta :name "viewport"
                   :content "width=device-width, initial-scale=1.0")
            (:meta :name "title"
                   :property "og:title"
                   :content "emile.space")
            (:meta :name "type"
                   :property "og:type"
                   :content "website")
            (:meta :name "image"
                   :property "og:image"
                   :content "https://emile.space/e.svg")
            (:meta :name "url"
                   :property "og:url"
                   :content "https://emile.space")
            (:title ,title)
            (:style (:raw (format nil "~%~{~a~%~}~%" (style "in/style.css"))))))
          (:body ,@body)))

(defun main ()
  (format t "~a~%"
    (with-page (:title "Home page")
               (:header
                 (:a :href "https://emile.spce" "emile.space")
                 (:a :href "https://r2wa.rs" "r2wa.rs"))
      (:nav
       (:ul
        (:li (:a :href "page.lisp" "page.lisp"))))

      (:ul
       (:li (:a :href "/about" "about"))
       (:li (:a :href "/blog" "blog")))

      (:span :id "emilespace")
      (:h1 (:a :href "#emilespace" "1. emile.space"))
      (:pre "
this is some
multiline text
and this line is aligned with the pre
and this one with the initial paren
      "))))

(main)

(defun starts-with-paren (line)
  (and (plusp (length line))
       (char= (char line 0) #\()))

(defun split-string (line)
  (remove "" (split-sequence:split-sequence #\Space line)))

(defun contains-token-starting-with-paren (line)
  (some (lambda (token)
          (and (plusp (length token))
               (char= (char token 0) #\()))
        (split-string line)))

(defun evaluate-line (line)
  (let* ((start (position #\( line))
         (base (subseq line start))
         (form (read-from-string base)))
    (format t "Evaluating: ~a~%" form)
    (format t "Result: ~a~%" (eval form))
    (format t "res: ~a~%" (concatenate 'string
                             (subseq line 0 start)
                             (princ-to-string (eval form))
                             (subseq line (+ start (length (princ-to-string form))))))))

(defun process-markdown-file (file-path)
  (with-open-file (stream file-path :direction :input)
    (loop for line = (handler-case (read line)
                                  (error (c)
                                         (format t "couldn't read form: ~a~%" c)
                                         (read-line stream nil nil)))
          while line do
            (when (or (starts-with-paren line)
                      (contains-token-starting-with-paren line))
              (evaluate-line line)))))

(process-markdown-file "in/sport.md")

(mapcar (lambda (d)
          (format t "~a:~%~{~a~%~}~%"
                  d
                  (with-open-file (stream d :direction :input)
                   (loop for line = (read-line stream nil nil)
                                 while line
                                 collect line))))
        (directory "in/*.md"))
