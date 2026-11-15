(defpackage :lun2
  (:use :cl))
(in-package :lun2)

;; (auto-revert-mode) in emacs is really nice in order to get live-reloading of the
;; generated html while running functions in a repl

(defconstant +hashtag+ #\#)

(defvar *previous-readtables* nil)

(defvar *heading-counters* nil
  "Stack of heading counters for hierarchical numbering.")

(defvar *current-lun-file* nil
  "Currently processing lun file path.")

(defvar *lun-metadata-cache* (make-hash-table :test #'equal)
  "A cache for the metadata")
(setq *lun-metadata-cache* (make-hash-table :test #'equal))

(defvar *lun-root-directory* nil)

(defvar *site-template* nil
  "Function that takes content string and metadata and returns html")

(defvar *current-toc* nil
  "Table of contents for the current file")

(defun reset-heading-counters ()
  "Reset heading counters to initial state."
  (setf *heading-counters* (make-array 10 :initial-element 0)))

(defun get-current-number (level)
  "Get the current heading number without incrementing"
  (with-output-to-string (s)
    (loop for i from 0 below level
          for count = (aref *heading-counters* i)
          when (plusp count)
          do (unless (zerop i) (write-char #\. s))
          (format s "~A" count))))

(defun get-heading-numberccccccncjr (level)
  "Get and update the heading number for the given level."
  (unless *heading-counters*
    (reset-heading-counters))
  ;; Increment counter at this level
  (incf (aref *heading-counters* (1- level)))
  ;; Reset all deeper levels
  (loop for i from level below (length *heading-counters*)
        do (setf (aref *heading-counters* i) 0))
  ;; Build the number string (e.g., "1.2.3")
  (with-output-to-string (s)
    (loop for i from 0 below level
          for count = (aref *heading-counters* i)
          when (plusp count)
          do (unless (zerop i) (write-char #\. s))
             (format s "~A" count))))

(defun make-heading-id (text)
  "Convert heading text to a valid HTML id: lowercase, no special chars."
  (with-output-to-string (s)
    (loop for char across (string-downcase text)
          when (or (alphanumericp char) (char= char #\-))
          do (write-char char s))))

(defun heading (level text)
  "Formats a given heading level and text as html"

  (format *error-output* "[DEBUG HEADING] Called with level=~A text=~A, *current-toc*=~A (length=~A)~%"
          level text (if *current-toc* "LIST" "NIL")
          (if *current-toc* (length *current-toc*) "N/A"))
  (when (listp *current-toc*)
   (let ((current-num (get-current-number level)))
      (push (list :level level :number current-num :text text :id (make-heading-id text))
         *current-toc*)
      (format *error-output* "[DEBUG TOC] Added heading: ~A ~A~%" current-num text))

   (let ((id (make-heading-id text))
         (number (get-heading-number level)))
     (format nil "<span id=\"~A\"></span>~%<h~A><a href=\"#~A\">~A ~A</a></h~A>~%" 
             id level id number text level))))

(defun bold (text)
  "Formats a given text as strong html"
  (format nil "<strong>~A</strong>" text))

(defun link (text target)
  "Format some given text and target as a html link"
  (format nil "<a href=\"~A\">~A</a>" target text))

(defun li (text)
  "Format text as a list item"
  (format nil "<li>~A</li>" text))

(defun read-heading (stream char)
  "Reader macro for # - reads rest of line and returns heading form.
   Supports multiple # for different heading levels (##, ###, etc.).
   The newline is preserved in the stream."
  (declare (ignore char))
  ;; Count consecutive # characters
  (let ((level 1))
    (loop while (eql (peek-char nil stream nil :eof) #\#)
          do (read-char stream)
             (incf level))
    ;; Read the rest of the line
    (let ((text (read-line stream nil "")))
      ;; Return form that concatenates heading and newline
      `(concatenate 'string
                    (heading ,level ,(string-trim '(#\Space #\Tab) text))
                    ,(string #\Newline)))))

(defun read-dash-list (stream char)
  "Reader macro for `-` reads the rest of the line into a `list-item` form"
  (declare (ignore char))
  (let ((text (read-line stream nil "")))
    `(concatenate 'string (li ,text) ,(string #\Newline))))

(defun read-bold (stream char)
  "Reader macro for * - reads until closing * and returns bold form."
  (declare (ignore char))
  (let ((text (with-output-to-string (s)
                (loop for c = (read-char stream nil :eof)
                      until (or (eql c :eof) (eql c #\*))
                      do (write-char c s)))))
    `(bold ,text)))

(defun read-link (stream char)
  "Reader macro for [ - reads [text](url) and returns link form."
  (declare (ignore char))
  ;; Read the link text until ]
  (let ((text (with-output-to-string (s)
                (loop for c = (read-char stream nil :eof)
                      until (or (eql c :eof) (eql c #\]))
                      do (write-char c s)))))
    ;; Next character should be (
    (let ((next-char (read-char stream nil :eof)))
      (if (eql next-char #\()
          ;; Read the URL until )
          (let ((url (with-output-to-string (s)
                       (loop for c = (read-char stream nil :eof)
                             until (or (eql c :eof) (eql c #\)))
                             do (write-char c s)))))
            ;; return the link
            `(link ,text ,url))
          ;; If no ( follows, just return the text with brackets
          (concatenate 'string "[" text (string next-char))))))

(defun enable-lun-syntax ()
  (push *readtable* *previous-readtables*)
  (setq *readtable* (copy-readtable))
  
  (set-macro-character +hashtag+ 'read-heading)
  (set-macro-character #\* 'read-bold nil)
  (set-macro-character #\[ 'read-link))
  ;(set-macro-character #\- 'read-dash-list))

(defun disable-lun-syntax ()
  (when *previous-readtables*
    (setq *readtable* (pop *previous-readtables*))))

(defun read-raw-text-chunk (stream)
  "Reads characters until EOF, #, *, [, or open-paren."
  (with-output-to-string (s)
    (loop
      (let ((c (peek-char nil stream nil :eof)))
        (cond
          ((eql c :eof) (return))
          ((or (eql c #\#) (eql c #\*) (eql c #\[) (eql c #\()) (return))
          (t (write-char (read-char stream) s)))))))

(defun read-lun-content (stream)
  "Parse stream into list of strings and forms.
   Use READ-PRESERVING-WHITESPACE to keep newlines after forms."
  (let ((forms nil))
    (loop
      (let ((c (peek-char nil stream nil :eof)))
        (cond
          ((eql c :eof) 
           (return (nreverse forms)))

          ;; #, *, [, or ( triggers reader - use READ-PRESERVING-WHITESPACE
          ((or (eql c #\#) (eql c #\*) (eql c #\[) (eql c #\())
           (let ((form (read-preserving-whitespace stream nil :eof)))
             (if (eql form :eof)
                 (return (nreverse forms))
                 (push form forms))))

          ;; Everything else is text
          (t 
           (let ((text (read-raw-text-chunk stream)))
             (when (plusp (length text))
               (push text forms)))))))))

(defun write-lun-output (parsed-content &optional (stream *standard-output*))
  "Write parsed content to stream, wrapping consecutive text in <p> tags."
  (let ((last-was-code nil)
        (text-buffer nil))
    (flet ((flush-text-buffer ()
             "Flush accumulated text as a paragraph."
             (when text-buffer
               (let ((combined (apply #'concatenate 'string (nreverse text-buffer))))
                 (when (and (> (length combined) 0)
                            (not (every (lambda (c)
                                          (member c '(#\Space #\Tab #\Newline)))
                                        combined)))
                   (format stream "<p>~A</p>~%~%" (string-trim '(#\Newline) combined))))
               (setf text-buffer nil))))
      (dolist (item parsed-content)
        (let ((is-code (not (stringp item))))
          (cond
            (is-code
             (flush-text-buffer)
             (let ((result (eval item)))
               (when (and result
                          (or (not (stringp result))
                              (> (length (string-trim '(#\Space #\Tab #\Newline) result)) 0)))
                 (format stream "~A" result)))
             (setf last-was-code t))
            (t
             (when (and last-was-code
                        (plusp (length item))
                        (not (member (char item 0) '(#\Space #\Tab #\Newline))))
               (format stream "~%"))
             (push item text-buffer)
             (setf last-was-code nil)))))
      (flush-text-buffer))))

(defun read-lun-hybrid (file-path &key (output-stream *standard-output*) debug)
  "Read and process a .lun file, outputting to specified stream."
  (when debug
    (format *error-output* "~%--- Processing ~A ---~%" file-path))
  
  (reset-heading-counters)
  (setf *current-toc* '())
  (format *error-output* "[DEBUG] Initialized *current-toc* to empty list~%")
  (let ((*current-lun-file* (pathname file-path))
        (*lun-root-directory* (or *lun-root-directory*
                                  (uiop:pathname-directory-pathname file-path)))
        (parsed-content nil))
      (setf parsed-content
            (progn
              (enable-lun-syntax)
              (unwind-protect
                   (with-open-file (stream file-path :direction :input)
                     (read-lun-content stream))
                (disable-lun-syntax))))
      (when debug
        (format *error-output* "[DEBUG] Forms List: ~S~%" parsed-content))

    (let ((content (with-output-to-string (s)
                     (write-lun-output parsed-content s))))
      (format *error-output* "[DEBUG] *current-toc* before storing: ~A entries~%" (length *current-toc*))
      (if *site-template*
          (let* ((metadata (gethash *current-lun-file* *lun-metadata-cache*))
                 (wrapped (funcall *site-template* content metadata)))
            (write-string wrapped output-stream))
          (write-string content output-stream)))

    (let ((toc-to-store (reverse (copy-tree *current-toc*))))
      (format *error-output* "[DEBUG] TOC content: ~A~%" toc-to-store)
      (format *error-output* "[DEBUG] Hash key: ~A~%" (cons *current-lun-file* :toc))
      (setf (gethash (cons *current-lun-file* :toc)
                     *lun-metadata-cache*)
            toc-to-store))

    (let ((retrieved (gethash (cons *current-lun-file* :toc) *lun-metadata-cache*)))
      (format *error-output* "[DEBUG] Stored TOC for ~A: ~A entries~%"
              *current-lun-file*
              (if retrieved (length retrieved) "NIL"))
      (format *error-output* "[DEBUG] Retrieved TOC: ~A~%" retrieved))

    (format *error-output* "[DEBUG] All cache keys:~%")
    (maphash (lambda (k v)
               (declare (ignore v)
                 (format *error-output* "  Key: ~A (type: ~A)~%" k (type-of k))))
       *lun-metadata-cache*)
    
    (when debug
      (format *error-output* "~%"))))

(defun lun-to-string (file-path)
  "Process .lun file and return result as string."
  (with-output-to-string (s)
    (read-lun-hybrid file-path :output-stream s)))

(defun lun-to-file (input-path output-path)
  "Process .lun file and write result to output file."
  (with-open-file (out output-path 
                       :direction :output 
                       :if-exists :supersede 
                       :if-does-not-exist :create)
    (read-lun-hybrid input-path :output-stream out)))

(defmacro meta (&rest plist)
  "Declare metadata for current file. Returns empty string so it doesn't appear in output.
   Example: (meta :title \"Some title\" :tags (event asia 2025) :date \"2025-03-15\")"
  `(progn
     (setf (gethash *current-lun-file* *lun-metadata-cache*)
           (list ,@plist))
     ""))

(defun find-lun-files (directory)
  "Recursively find all .lun files in directory."
  (let ((files nil))
    (labels ((walk (dir)
               (dolist (entry (uiop:directory-files dir))
                 (when (string= (pathname-type entry) "lun")
                   (push entry files)))
               (dolist (subdir (uiop:subdirectories dir))
                 (walk subdir))))
      (walk (uiop:ensure-directory-pathname directory)))
    files))

(defun ensure-metadata-loaded (file-path)
  "Ensure metadata for a file is loaded. Parse just the first form if it's a meta call."
  (unless (gethash file-path *lun-metadata-cache*)
    (enable-lun-syntax)
    (unwind-protect
         (with-open-file (stream file-path :direction :input)
           (let ((*current-lun-file* file-path))
             (let ((first-form (read stream nil :eof)))
               (when (and (consp first-form)
                         (eq (car first-form) 'meta))
                 (eval first-form))
               (unless (gethash file-path *lun-metadata-cache*)
                 (setf (gethash file-path *lun-metadata-cache*) nil)))))
      (disable-lun-syntax))))

(defun scan-directory-metadata (directory)
  "Scan all .lun files in directory and load their metadata."
  (setf *lun-root-directory* directory)
  (dolist (file (find-lun-files directory))
    (ensure-metadata-loaded file)))

(defun get-metadata (file-path key &optional default)
  "Get metadata value for a file."
  (ensure-metadata-loaded file-path)
  (getf (gethash file-path *lun-metadata-cache*) key default))

(defun has-tag-p (file-path tag)
  "Check if file has a specific tag."
  (let ((tags (get-metadata file-path :tags)))
    (member tag tags)))

(defun query-files (&key tags any-tag title-contains sort-by)
  "Query files based on metadata criteria.
   :tags - list of tags (all must match)
   :any-tag - list of tags (any must match)
   :title-contains - substring that must appear in title
   :sort-by - :date, :title, or nil"
  (let ((results nil))
    (maphash (lambda (file-path metadata)
               (when (pathnamep file-path)
                (let ((matches t))
                  ;; Check tags (all must match)
                  (when tags
                    (let ((file-tags (getf metadata :tags)))
                      (unless (every (lambda (tag) (member tag file-tags)) tags)
                        (setf matches nil))))
                 
                  ;; Check any-tag (at least one must match)
                  (when any-tag
                    (let ((file-tags (getf metadata :tags)))
                      (unless (some (lambda (tag) (member tag file-tags)) any-tag)
                        (setf matches nil))))
                 
                  ;; Check title
                  (when title-contains
                    (let ((title (getf metadata :title "")))
                      (unless (search title-contains title :test #'char-equal)
                        (setf matches nil))))
                 
                  (when matches
                    (push (list :file file-path :metadata metadata) results)))))
             *lun-metadata-cache*)
    
    ;; Sort results
    (when sort-by
      (setf results
            (sort results
                  (lambda (a b)
                    (let ((meta-a (getf a :metadata))
                          (meta-b (getf b :metadata)))
                      (cond
                        ((eq sort-by :date)
                         (string< (getf meta-a :date "")
                                 (getf meta-b :date "")))
                        ((eq sort-by :title)
                         (string< (getf meta-a :title "")
                                 (getf meta-b :title "")))
                        (t nil)))))))
    
    (nreverse results)))

(defun file-to-html-path (lun-file-path)
  "Convert .lun file path to .html output path."
  (make-pathname :type "html" :defaults lun-file-path))

(defun relative-link (from-file to-file)
  "Generate relative link from one file to another."
  (let* ((from-html (file-to-html-path from-file))
         (to-html (file-to-html-path to-file))
         (from-dir (uiop:pathname-directory-pathname from-html))
         (rel-path (enough-namestring to-html from-dir)))
    rel-path))

(defun render-file-link (file-path &key (link-text nil))
  "Render HTML link to another lun file (converted to .html).
   Uses file title as link text if not provided."
  (let* ((title (or link-text 
                   (get-metadata file-path :title)
                   (pathname-name file-path)))
         (relative-url (relative-link *current-lun-file* file-path)))
    (format nil "<a href=\"~A\">~A</a>" relative-url title)))
 
(defun render-file-list (files &key (show-date t) (show-tags nil) (show-description t))
  "Render a list of files as HTML."
  (with-output-to-string (s)
    (format s "<ul class=\"file-list\">~%")
    (dolist (file-info files)
      (let* ((file-path (getf file-info :file))
             (metadata (getf file-info :metadata))
             (title (getf metadata :title (pathname-name file-path)))
             (date (getf metadata :date))
             (tags (getf metadata :tags))
             (description (getf metadata :description)))
        
        (format s "  <li>~%")
        (format s "    ~A" (render-file-link file-path :link-text title))
        
        (when (and show-date date)
          (format s " <span class=\"date\">(~A)</span>" date))
        
        (when (and show-tags tags)
          (format s " <span class=\"tags\">[~{~A~^, ~}]</span>" tags))
        
        (when (and show-description description)
          (format s "<br>~%    <span class=\"description\">~A</span>" description))
        
        (format s "~%  </li>~%")))
    (format s "</ul>~%")))

(defun substitute-string (string old new)
   "Replace all occurrences of OLD with NEW in STRING."
   (let ((pos (search old string)))
      (if pos
          (concatenate 'string
                       (subseq string 0 pos)
                       new
                       (substitute-string
                          (subseq string (+ pos (length old)))
                          old
                          new))
          string)))

(defun extract-links-from-file (file-path)
  "Extract all render-file-link calls from a file by parsing it."
  (let ((links nil)
        (*current-lun-file* file-path)
        (file-dir (uiop:pathname-directory-pathname file-path)))
    (format *error-output* "[DEBUG BACKLINKS] Scanning ~A for links~%" file-path)
    (enable-lun-syntax)
    (unwind-protect
         (with-open-file (stream file-path :direction :input)
           (handler-case
               (let ((forms (read-lun-content stream)))
                 (format *error-output* "[DEBUG BACKLINKS] Found ~A forms in ~A~%" (length forms) file-path)
                 (labels ((extract-from-form (form)
                            (cond
                              ((not (consp form)) nil)
                              ((eq (car form) 'render-file-link)
                               (let ((target-form (second form)))
                                 (format *error-output* "[DEBUG BACKLINKS] Found render-file-link: ~A~%" target-form)
                                 (handler-case
                                     (let ((target (eval target-form)))
                                       (format *error-output* "[DEBUG BACKLINKS] Evaluated to: ~A~%" target)
                                       (when (pathnamep target)
                                         (push target links)))
                                   (error (e)
                                     (format *error-output* "[DEBUG BACKLINKS] Error evaluating: ~A~%" e)))))
                              ((eq (car form) 'link)
                               (let ((url-form (third form)))
                                 (format *error-output* "[DEBUG BACKLINKS] Found link with URL: ~A~%" url-form)
                                 (when (stringp url-form)
                                   (cond
                                     ((search ".lun" url-form)
                                      (let ((target-path (merge-pathnames url-form file-dir)))
                                        (format *error-output* "[DEBUG BACKLINKS] Resolved .lun link to: ~A~%" target-path)
                                        (push (truename target-path) links)))
                                     ((search ".html" url-form)
                                      (let* ((lun-url (substitute-string url-form ".html" ".lun"))
                                             (target-path (merge-pathnames lun-url file-dir)))
                                        (handler-case
                                            (let ((resolved (truename target-path)))
                                              (format *error-output* "[DEBUG BACKLINKS] Resolved .html link to: ~A~%" resolved)
                                              (push resolved links))
                                          (error (e)
                                            (format *error-output* "[DEBUG BACKLINKS] Could not resolve ~A: ~A~%" target-path e)))))))))
                              (t
                               (dolist (subform form)
                                 (when (consp subform)
                                   (extract-from-form subform)))))))
                   (dolist (form forms)
                     (extract-from-form form))))
             (error (e)
               (format *error-output* "[DEBUG BACKLINKS] Error reading file: ~A~%" e))))
      (disable-lun-syntax)
      (format *error-output* "[DEBUG BACKLINKS] Found ~A links in ~A~%" (length links) file-path))
    links))

(defun table-of-contents ()
  "Generate the table of contents for the current file"
  (format *error-output* "[DEBUG TOC] Looking up TOC for ~A~%" *current-lun-file*)
  (format *error-output* "[DEBUG TOC] Looking up key: ~A~%" (cons *current-lun-file* :toc))
  (let ((toc (gethash (cons *current-lun-file* :toc) *lun-metadata-cache*)))
    (format *error-output* "[DEBUG TOC] Found TOC: ~A~%" toc)
    (if toc
        (with-output-to-string (s)
          (format s "<nav class=\"toc\">~%~t<h3>Table Of Contents</h3>~%~t<ul class=\"toc\">~%")
          (let ((prev-level 1))
            (dolist (entry toc)
              (let ((level (getf entry :level))
                    (number (getf entry :number))
                    (text (getf entry :text))
                    (id (getf entry :id)))
                (format t "[DEBUG TOC VERBOSE] level: ~A number: ~A text: ~A id: ~A~%" level number text id)
                (cond
                  ((> level prev-level)
                   (format s "~vt<ul~A>~%" level (1- level)))
                  ((< level prev-level)
                   (format s "~vt</ul~A>~%" (1+ level) level)))
                (format s "~vt<li><a href=\"#~A\">~A ~A</a></li>~%" level id number text)
                (setf prev-level level))))
          (format s "~t</ul>~%</nav>~%~%"))
        "")))
            
(defun find-backlinks (file-path)
  "Find all files that link to the given file"
  (format *error-output* "[DEBUG BACKLINKS] Finding backlinks to ~A~%" file-path)
  (let ((backlinks nil))
    (maphash (lambda (key metadata)
               (declare (ignore metadata))
               (when (pathnamep key)
                 (let ((links (extract-links-from-file key)))
                   (when (member file-path links :test #'equal)
                     (push key backlinks)))))
             *lun-metadata-cache*)
    (format *error-output* "[DEBUG BACKLINKS] Total backlinks found: ~A~%" (length backlinks))
    backlinks))

(defun backlinks ()
  "Generate backlinks section for the current file"
  (format *error-output* "[DEBUG BACKLINKS] Generating backlinks for ~A~%" *current-lun-file*)
  (let ((links (find-backlinks *current-lun-file*)))
    (if links
        (with-output-to-string (s)
          (format s "<nav class=\"backlinks\">~%<h3>Backlinks</h3>~%<ul>~%")
          (dolist (link-file links)
            (let* ((metadata (gethash link-file *lun-metadata-cache*))
                   (title (getf metadata :title (pathname-name link-file)))
                   (relative-url (relative-link *current-lun-file* link-file)))
              (format s "<li><a href=\"~A\">~A</a></li>~%" relative-url title)))
          (format s "</ul>~%</nav>~%~%"))
        "")))

(defun set-template (template-function)
  "Set the site template function, accepts (content metadata) and returns wrapped HTML string"
  (setf *site-template* template-function))

(defmacro query-and-list (&key tags any-tag title-contains sort-by 
                               (show-date t) (show-tags nil) (show-description t))
  "Query files and render as HTML list.
   Example: (query-and-list :tags '(event) :sort-by :date)"
  `(let ((results (query-files :tags ',tags 
                               :any-tag ',any-tag
                               :title-contains ,title-contains
                               :sort-by ,sort-by)))
     (render-file-list results 
                      :show-date ,show-date
                      :show-tags ,show-tags
                      :show-description ,show-description)))


(defun build-site (input-directory output-directory)
  "Build entire site: scan metadata, process all files.
   First pass: scan metadata and generate toc
   Second pass: render with templates and backlinks"
  (setf *lun-root-directory* (uiop:ensure-directory-pathname input-directory))
  (format t "Scanning metadata...~%")
  (scan-directory-metadata input-directory)
  (format t "Found ~A files~%" (hash-table-count *lun-metadata-cache*))
  
  (format t "Processing files (Pass 1: TOC)...~%")
  (maphash (lambda (file-path metadata)
             (declare (ignore metadata))
             (when (pathnamep file-path)
               (let ((*current-lun-file* file-path))
                (lun-to-string file-path))))
           *lun-metadata-cache*)
  
  (format t "Processing files (pass 2 - output)...~%")
  (maphash (lambda (file-path metadata)
             (declare (ignore metadata))
             (when (pathnamep file-path)
               (let* ((rel-path (enough-namestring file-path *lun-root-directory*))
                      (output-file (merge-pathnames
                                    (make-pathname :type "html" :defaults rel-path)
                                    output-directory)))
                 (ensure-directories-exist output-file)
                 (format t "  ~A -> ~A~%"
                         (enough-namestring file-path *lun-root-directory*)
                         (enough-namestring output-file output-directory))
                 (lun-to-file file-path output-file))))
           *lun-metadata-cache*)
  (format t "Done!~%"))
