(in-package :vokodeep)

(defstruct metadata title tags)

(defun split-into-blocks (content)
  "Split content into :text or :code blocks"
  (let ((blocks '())
        (current-type :text)
        (current-content ""))
    (loop for line in (split-sequence:split-sequence #\Newline content) do
      (cond
        ((and (eq current-type :text) (alexandria:starts-with-subseq "```" line))
         (push `(,current-type ,current-content) blocks)
         (setf current-type :code current-content ""))
        ((and (eq current-type :code) (alexandria:starts-with-subseq "```" line))
         (push `(,current-type ,current-content) blocks)
         (setf current-type :text current-content ""))
        (t (setf current-content (uiop:strcat current-content line #\Newline)))))
    (push `(,current-type ,current-content) blocks)
    (nreverse blocks)))

(defun eval-lisp-form (text &optional (metadata (make-metadata)))
  "Evaluate all valid Lisp forms in text, ignoring code blocks"
  (let ((pos 0)
        (output (make-string-output-stream)))
    (loop while (< pos (length text)) do
      (let ((next-paren (position #\( text :start pos)))
        (cond
          ((null next-paren)
           (write-string (subseq text pos) output)
           (setf pos (length text)))
          (t
           (write-string (subseq text pos next-paren) output)
           (multiple-value-bind (form new-pos)
               (ignore-errors (read-from-string text nil nil :start next-paren))
             (if form
                 (let ((result (ignore-errors (eval form))))
                   (typecase result
                     (metadata (setf metadata result))
                     (t (princ result output))))
                 (princ #\( output))
             (setf pos (or new-pos (1+ next-paren))))))))
    (values (get-output-stream-string output) metadata)))

(defun process-markdown (content)
  "Process Markdown, evaluating Lisp forms and returning (text metadata)"
  (let ((blocks (split-into-blocks content))
        (metadata (make-metadata))
        (output ""))
    (dolist (block blocks)
      (destructuring-bind (type content) block
        (if (eq type :code)
            (setf output (uiop:strcat output "```" content "```"))
            (multiple-value-bind (processed new-metadata)
                (eval-lisp-form content metadata)
              (setf output (uiop:strcat output processed)
                    metadata new-metadata)))))
    (values output metadata)))

;; 2. Plugin System & HTML Transpiler
(defvar *plugins* (make-hash-table)) ; Key: plugin name, Value: function

(defmacro define-plugin (name (input metadata) &body body)
  "Define a plugin to transform HTML content."
  `(setf (gethash ,name *plugins*)
         (lambda (,input ,metadata) ,@body)))

(defun apply-plugins (html metadata)
  "Run all plugins on the HTML."
  (loop for plugin being the hash-values of *plugins*
        do (setf html (funcall plugin html metadata)))
  html)

(defun markdown-to-html (processed-content metadata)
  "Convert processed Markdown to HTML, applying plugins"
  (let ((html (format nil "<!DOCTYPE html><html><head></head><body>~a</body></html>" processed-content)))
    (apply-plugins html metadata)))

;; 3. File Processing & User API
(defun process-file (input-path output-dir)
  "Process a single Markdown file and save HTML."
  (let* ((content (alexandria:read-file-into-string input-path))
         (output-path
           (merge-pathnames
            (make-pathname :directory (pathname-directory output-dir)
                           :name (pathname-name input-path)
                           :type "html"))))
    (format t "~a => ~a~%" input-path output-path)
    (multiple-value-bind (processed-content metadata)
        (process-markdown content)
      (alexandria:write-string-into-file
       (markdown-to-html processed-content metadata)
       output-path :if-exists :supersede))))

(defun process-folder (input-dir output-dir)
  "Process all Markdown files in a folder."
  (ensure-directories-exist output-dir)
  (let ((files (directory (merge-pathnames "*.md" input-dir))))
    (mapc #'bt:join-thread
          (loop for file in files
                collect (bt:make-thread (lambda ()
                                                (format t "processing ~a~%" file
                                                 (process-file file output-dir))))))))
  
  ;(dolist (file (directory (merge-pathnames "*.md" input-dir)))
    ;(format t "~a -> ~a~%" file output-dir)
    ;(process-file file output-dir)))

;; Example Plugins (User defined)
;; Backlinks
(define-plugin :backlinks (html metadata)
  (declare (ignore metadata))
  (cl-ppcre:regex-replace-all "</body>" html "<div class='backlinks'>backlinks</div></body>"))

(define-plugin :toc (html metadata)
  (declare (ignore metadata))
  (let ((toc "<nav><h2><Table of Contents/h2></nav>"))
    (cl-ppcre:regex-replace-all "<body>" html (uiop:strcat "<body>" toc))))

(define-plugin :tags (html metadata)
  (if (metadata-tags metadata)
      (format nil "~a<div>Tags: ~{~a~^, ~}<div>" html (metadata-tags metadata))
      html))

(process-folder #P"./in/" #P"./out/")

;; HTML foo

(defun text-node (content) `(:text ,content))
(defun elem (tag attrs &rest children) `(,tag ,attrs ,@children))
(defun attr (key value) `(,key ,value))

(format t "~a~%"
        (elem :div (list (attr :class "content"))
              (elem :h1 () (text-node "Hello World"))))

;; Prolog

(defstruct (index-entry (:conc-name entry-)) 
  metadata ir path)

(defvar *index* (make-hash-table :test 'equal))
(defvar *index-lock* (bt:make-lock))

(defmacro *- (name args &body body)
  `(defun ,name ,args ,@body))

(defun ?- (&rest predicates)
  (bt:with-lock-held (*index-lock*)
    (loop for entry being the hash-values of *index*
          when (every (lambda (pred) (funcall pred entry)) predicates)
            collect entry)))

(*- has-tag (tag entry)
  (member tag (metadata-tags (entry-metadata entry)) :test #'string=))

;; HTML gen

(defun ir->html (ir)
  "Converts the given Intermediate Representation to HTML"
  (labels ((escape (str) str)
           (attrs->str (attrs)
             (format nil "~{~a=\"~a\"~^ ~}"
                     (mapcan (lambda (a) (list (car a) (cadr a))) attrs)))
           (render (node)
             (cond
               ((null node) "")
               ((atom node) (escape (princ-to-string node)))
               ((eql (first node) :text)
                (escape (second node)))
               (t (let* ((tag (first node))
                         (attrs (second node))
                         (children (mapcar #'render (cddr node)))
                         (content (format nil "~{~a~}" children)))
                    (if (member tag '(:img :br :hr :meta :link :input))
                        (format nil "<~a~@[~a~]/>" tag (attrs->str attrs))
                        (format nil "<~a~@[~a~]>~a</~a>"
                                tag (attrs->str attrs) content tag)))))))
    (render ir)))

(format t "~a~%" (ir->html
                        (elem :div (list (attr :class "content"))
                              (elem :h1 () (text-node "Hello World")))))

