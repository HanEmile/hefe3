(uiop:define-package :vokobe3
  (:use :cl)
  (:export #:sync #:watch #:render-page #:main))

(in-package :vokobe3)

;; --- State ---
(defvar *input-dir* nil)
(defvar *output-dir* "out/")
(defvar *inter-dir* "inter/")
(defvar *tags* (make-hash-table :test 'equal))
(defvar *links* (make-hash-table :test 'equal)) ; Track links between pages
(defvar *heading-numbers* nil) ; Track heading numbers for current page

;; --- The Readtable ---
(defvar *wiki-readtable* (let ((rt (copy-readtable nil)))
    (set-syntax-from-char (code-char 160) #\Space rt)
    (set-syntax-from-char #\: #\a rt)
    (dolist (c '(#\` #\|)) 
      (set-syntax-from-char c #\a rt))
    rt))

;; --- Parsing Logic ---

(defun read-markdown-file (path)
  "Buffer-safe parser that handles multi-line Lisp and plain Markdown."
  (let* ((raw (uiop:read-file-string path))
         (content (cl-ppcre:regex-replace-all (code-char 160) raw " "))
         (len (length content))
         (index 0)
         (forms nil)
         (in-code-block nil))
    (loop while (< index len) do
      (let* ((line-end (or (position #\Newline content :start index) len))
             (line (subseq content index line-end))
             (trimmed (string-trim '(#\Space #\Tab #\Return) line)))
        (cond
          ;; Code block toggle
          ((uiop:string-prefix-p "```" trimmed)
           (setf in-code-block (not in-code-block))
           (push `(code-fence ,trimmed) forms)
           (setf index (1+ line-end)))
          
          ;; Inside code block - treat as verbatim
          (in-code-block
           (push `(code-line ,line) forms)
           (setf index (1+ line-end)))
          
          ;; Empty line
          ((string= trimmed "") 
           (setf index (1+ line-end)))
          
          ;; Wiki directives
          ((uiop:string-prefix-p ":::toc" trimmed)
           (push `(toc) forms)
           (setf index (1+ line-end)))
          
          ((uiop:string-prefix-p ":::backlinks" trimmed)
           (push `(backlinks) forms)
           (setf index (1+ line-end)))
          
          ;; Alternative tag syntax [:tag ...]
          ((uiop:string-prefix-p "[:tag " trimmed)
           (let* ((content (subseq trimmed 6))
                  (end-pos (position #\] content)))
             (when end-pos
               (let ((tag-string (subseq content 0 end-pos)))
                 (push `(tag ,@(mapcar #'intern (uiop:split-string tag-string))) forms))))
           (setf index (1+ line-end)))
          
          ;; Lisp Forms
          ((and (uiop:string-prefix-p "(" trimmed)
                (let ((first-word (second (cl-ppcre:split "[\\(\\s:]+" trimmed))))
                  (and first-word 
                       (member (string-downcase first-word) 
                               '("tag" "link" "let" "defun" "in-package") 
                               :test #'string=))))
           (multiple-value-bind (form next-pos)
               (let ((*package* (find-package :vokobe3))
                     (*readtable* (copy-readtable nil)))
                 (read-from-string content nil :eof :start index))
             (push form forms)
             (setf index next-pos)))

          ;; Headers
          ((uiop:string-prefix-p "### " trimmed)
           (push `(h3 ,(subseq trimmed 4)) forms)
           (setf index (1+ line-end)))
          
          ((uiop:string-prefix-p "## " trimmed)
           (push `(h2 ,(subseq trimmed 3)) forms)
           (setf index (1+ line-end)))
          
          ((uiop:string-prefix-p "# " trimmed)
           (push `(h1 ,(subseq trimmed 2)) forms)
           (setf index (1+ line-end)))
          
          ;; Blockquote
          ((uiop:string-prefix-p "> " trimmed)
           (push `(blockquote ,(subseq trimmed 2)) forms)
           (setf index (1+ line-end)))
          
          ;; Default to Paragraph
          (t 
           (push `(p ,line) forms)
           (setf index (1+ line-end))))))
    (nreverse forms)))

;; --- Rendering & Interpolation ---

(defun reset-heading-numbers ()
  "Reset heading numbers to start a new document"
  (setf *heading-numbers* (list 0 0 0))) ; Use list constructor, not quoted literal

(defun increment-heading (level)
  "Increment heading counter at given level and reset lower levels"
  (case level
    (1 (progn
         (incf (nth 0 *heading-numbers*))
         (setf (nth 1 *heading-numbers*) 0)
         (setf (nth 2 *heading-numbers*) 0)))
    (2 (progn
         (incf (nth 1 *heading-numbers*))
         (setf (nth 2 *heading-numbers*) 0)))
    (3 (incf (nth 2 *heading-numbers*)))))

(defun get-heading-number (level)
  "Get the current heading number as a string like '1.2.3'"
  (case level
    (1 (format nil "~D" (nth 0 *heading-numbers*)))
    (2 (format nil "~D.~D" (nth 0 *heading-numbers*) (nth 1 *heading-numbers*)))
    (3 (format nil "~D.~D.~D" 
               (nth 0 *heading-numbers*) 
               (nth 1 *heading-numbers*) 
               (nth 2 *heading-numbers*)))))

;; --- Rendering & Interpolation ---

(defun convert-markdown-links (text)
  "Convert [text](link) to <a href='link'>text</a> and track internal links"
  (cl-ppcre:regex-replace-all 
   "\\[([^\\]]+)\\]\\(([^\\)]+)\\)"
   text
   (lambda (match link-text link-target)
     (declare (ignore match))
     ;; Track internal links (those without :// in them)
     (unless (search "://" link-target)
       (when *load-pathname*
         (let ((source (pathname-name *load-pathname*)))
           (push source (gethash link-target *links* nil)))))
     ;; Return the HTML link
     (if (search "://" link-target)
         (format nil "<a href='~A'>~A</a>" link-target link-text)
         (format nil "<a href='~A.html'>~A</a>" link-target link-text)))
   :simple-calls t))

(defun interpolate (text)
  (if (not (stringp text))
      text
      (let ((text-with-links (convert-markdown-links text)))
        (cl-ppcre:regex-replace-all 
         "\\(([^)]+)\\)" 
         text-with-links
         (lambda (match inner)
           (let* ((form (handler-case 
                            (let ((*package* (find-package :vokobe3)))
                              (read-from-string (format nil "(~A)" inner)))
                          (error () nil))))
             (cond
               ((and (listp form) (symbolp (car form)) (fboundp (car form)))
                (handler-case (format nil "~A" (eval form))
                  (error (c) (format nil "[Error: ~A]" c))))
               
               ((and (symbolp form) (fboundp form))
                (format nil "~A" (funcall form)))

               (t match))))
         :simple-calls t))))

(defun generate-toc (forms)
  "Generate table of contents from headers in forms"
  (let ((headers nil)
        (temp-numbers (list 0 0 0))) ; Use list constructor instead of quoted literal
    (dolist (form forms)
      (when (and (listp form)
                 (member (car form) '(h1 h2 h3)))
        (let* ((level (case (car form)
                       (h1 1)
                       (h2 2)
                       (h3 3)))
               (text (cadr form))
               (id (cl-ppcre:regex-replace-all "[^a-z0-9]+" 
                                               (string-downcase text) 
                                               "-")))
          ;; Update temp numbering
          (case level
            (1 (progn
                 (incf (nth 0 temp-numbers))
                 (setf (nth 1 temp-numbers) 0)
                 (setf (nth 2 temp-numbers) 0)))
            (2 (progn
                 (incf (nth 1 temp-numbers))
                 (setf (nth 2 temp-numbers) 0)))
            (3 (incf (nth 2 temp-numbers))))
          
          (let ((number (case level
                         (1 (format nil "~D" (nth 0 temp-numbers)))
                         (2 (format nil "~D.~D" (nth 0 temp-numbers) (nth 1 temp-numbers)))
                         (3 (format nil "~D.~D.~D" 
                                   (nth 0 temp-numbers) 
                                   (nth 1 temp-numbers) 
                                   (nth 2 temp-numbers))))))
            (push (list level number text id) headers)))))
    (when headers
      (format nil "<nav class='toc'><h3>Contents</h3><ul>~{~A~}</ul></nav>"
              (mapcar (lambda (h)
                       (format nil "<li class='toc-~A'><a href='#~A'>~A ~A</a></li>"
                              (first h) (fourth h) (second h) (third h)))
                     (nreverse headers))))))

(defun generate-backlinks (page-name)
  "Generate backlinks section for a page"
  (let ((backlinks (gethash page-name *links*)))
    (if backlinks
        (format nil "<section class='backlinks'><h2>Backlinks</h2><ul>~{~A~}</ul></section>"
                (mapcar (lambda (source)
                         (format nil "<li><a href='~A.html'>~A</a></li>" source source))
                       (remove-duplicates backlinks :test #'string=)))
        "<section class='backlinks'><h2>Backlinks</h2><p>No backlinks yet.</p></section>")))

(defvar *current-code-block* nil)

(defun render-form (form)
  (cond
    ((null form) "")
    ((stringp form) (interpolate form))
    ((listp form)
     (let ((op (car form)))
       (case op
         (h1 (let* ((text (interpolate (cadr form)))
                   (id (cl-ppcre:regex-replace-all "[^a-z0-9]+" 
                                                   (string-downcase text) 
                                                   "-")))
              (increment-heading 1)
              (format nil "<h1 id='~A'>~A ~A</h1>" 
                     id 
                     (get-heading-number 1) 
                     text)))
         (h2 (let* ((text (interpolate (cadr form)))
                   (id (cl-ppcre:regex-replace-all "[^a-z0-9]+" 
                                                   (string-downcase text) 
                                                   "-")))
              (increment-heading 2)
              (format nil "<h2 id='~A'>~A ~A</h2>" 
                     id 
                     (get-heading-number 2) 
                     text)))
         (h3 (let* ((text (interpolate (cadr form)))
                   (id (cl-ppcre:regex-replace-all "[^a-z0-9]+" 
                                                   (string-downcase text) 
                                                   "-")))
              (increment-heading 3)
              (format nil "<h3 id='~A'>~A ~A</h3>" 
                     id 
                     (get-heading-number 3) 
                     text)))
         (p  (format nil "<p>~A</p>" (interpolate (cadr form))))
         (blockquote (format nil "<blockquote>~A</blockquote>" (interpolate (cadr form))))
         (code-fence 
          (if *current-code-block*
              (progn 
                (setf *current-code-block* nil)
                "</code></pre>")
              (progn
                (setf *current-code-block* t)
                "<pre><code>")))
         (code-line (format nil "~A~%" (cadr form)))
         ;; TOC and backlinks are handled in render-page
         (tag "")
         (in-package "")
         ;; Don't render defun directly
         (defun "")
         (t (handler-case 
                (let ((*package* (find-package :vokobe3)))
                  (let ((res (eval form)))
                    (if res (format nil "~A" res) "")))
              (error () ""))))))
    (t (format nil "~A" form))))

(defun load-css ()
  "Load CSS from in/style.css if it exists, otherwise return empty string"
  (let ((css-path (merge-pathnames "style.css" *input-dir*)))
    (if (probe-file css-path)
        (uiop:read-file-string css-path)
        "")))

(defun render-page (body)
  (reset-heading-numbers) ; Initialize numbering for this page
  (let* ((page-name (if *load-pathname* (pathname-name *load-pathname*) "index"))
         (toc-html (generate-toc body))
         (backlinks-html (generate-backlinks page-name))
         (css (load-css)))
    (reset-heading-numbers) ; Reset again before rendering (TOC used temp numbers)
    
    ;; Build the page structure
    (let* ((header-html (format nil "<header><nav><a href='/index.html'>Home</a></nav></header>"))
           (rendered-parts
            (loop for form in body
                  for i from 0
                  for rendered = (cond
                                  ;; Explicit TOC directive - insert TOC
                                  ((and (listp form) (eq (car form) 'toc))
                                   toc-html)
                                  ;; Explicit backlinks directive - insert backlinks
                                  ((and (listp form) (eq (car form) 'backlinks))
                                   backlinks-html)
                                  ;; Otherwise render normally
                                  (t (render-form form)))
                  unless (or (null rendered) (string= rendered ""))
                  collect rendered
                  ;; After first heading (h1), insert TOC automatically if not already present
                  when (and (= i 0) 
                           (listp form) 
                           (eq (car form) 'h1)
                           (not (find-if (lambda (f) (and (listp f) (eq (car f) 'toc))) body)))
                  collect toc-html))
           ;; Add backlinks at the end if not explicitly placed
           (final-parts (if (find-if (lambda (f) (and (listp f) (eq (car f) 'backlinks))) body)
                           rendered-parts
                           (append rendered-parts (list backlinks-html)))))
      (format nil "<html><head><meta charset='utf-8'><style>~A</style></head><body>~A<main>~{~A~}</main></body></html>" 
              css
              header-html
              final-parts))))

(defmacro tag (&rest tags)
  "Register tags for the current file. Returns empty string for rendering."
  `(progn
     (let ((name (if *load-pathname* (pathname-name *load-pathname*) 
                     "index")))
       (setf (gethash name *tags*) ',tags))
     ""))

(defun list-tags-inline ()
  "List all tags as a comma-separated string."
  (let ((all-tags (make-hash-table :test 'equal)))
    (maphash (lambda (k v)
               (dolist (tag v)
                 (setf (gethash tag all-tags) t)))
             *tags*)
    (if (zerop (hash-table-count all-tags))
        "none"
        (format nil "~{~A~^, ~}" 
                (sort (loop for k being the hash-keys of all-tags collect k)
                      #'string<)))))

(defun link (target &optional (text target))
  "Generate an HTML link."
  (when *load-pathname*
    (let ((source (pathname-name *load-pathname*)))
      (push source (gethash target *links* nil))))
  (format nil "<a href='~A.html'>~A</a>" target text))

;; --- System Operations ---

(defun sync ()
  (ensure-directories-exist *inter-dir*)
  (ensure-directories-exist *output-dir*)
  ;; Clear state
  (setf *tags* (make-hash-table :test 'equal))
  (setf *links* (make-hash-table :test 'equal))
  
  (let ((files (uiop:directory-files *input-dir* "*.md")))
    ;; PASS 1: Scan all files, generate .lisp, and LOAD them into the environment
    (dolist (file files)
      (let* ((name (pathname-name file))
             (forms (read-markdown-file file))
             (inter-path (merge-pathnames (format nil "~A.lisp" name) *inter-dir*)))
        (with-open-file (s inter-path :direction :output :if-exists :supersede)
          (format s "(in-package :vokobe3)~%")
          (dolist (f forms)
            ;; Only write executable logic to the intermediate file
            (when (and (listp f) (member (car f) '(tag defun let in-package format) :test #'string-equal))
              (print f s))))
        ;; Load the file so the *tags* hash table and functions are populated
        (let ((*load-pathname* file)
              (*package* (find-package :vokobe3)))
          (load inter-path))))
    
    ;; PASS 2: Now that the global state (*tags*, *links*) is ready, render everything
    (dolist (file files)
      (let* ((name (pathname-name file))
             (forms (read-markdown-file file))
             (out-path (merge-pathnames (format nil "~A.html" name) *output-dir*)))
        (with-open-file (s out-path :direction :output :if-exists :supersede)
          (let ((*load-pathname* file))
            (write-string (render-page forms) s)))))
    (format t ">> Site synchronized successfully.~%")))

(defun watch (path)
  (let ((input-path (uiop:ensure-directory-pathname (merge-pathnames "in/" path)))
        (last-build-time (make-hash-table :test 'equal)))
    (setf *input-dir* input-path)
    (format t "--- Monitoring: ~A ---~%" *input-dir*)
    (loop
      (let ((needs-rebuild nil))
        (dolist (file (uiop:directory-files *input-dir* "*.md"))
          (let ((mtime (file-write-date file)))
            (when (> mtime (gethash file last-build-time 0))
              (setf needs-rebuild t)
              (setf (gethash file last-build-time) mtime))))
        (when needs-rebuild
          (sync)
          (format t ">> Rebuilding...~%")))
      (sleep 1))))

(defun main ()
  (let ((args (uiop:command-line-arguments)))
    (setf *input-dir* (uiop:ensure-directory-pathname (or (car (last args)) "in/")))
    (if (member "watch" args :test #'string-equal)
        (watch *input-dir*)
        (sync)))
  (uiop:quit 0))
