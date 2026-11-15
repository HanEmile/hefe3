(defpackage :lun3
  (:use :cl))
(in-package :lun3)

;;; ============================================================================
;;; CONSTANTS
;;; ============================================================================

(defconstant +hashtag+ #\#)
(defconstant +cache-filename+ ".lun-cache.lisp")

;;; ============================================================================
;;; CONTEXT STRUCTURE
;;; ============================================================================

(defstruct lun-context
  "Immutable context for lun processing."
  (root-directory nil :type (or null pathname))
  (current-file nil :type (or null pathname))
  (metadata-cache (make-hash-table :test #'equal) :type hash-table)
  (site-template nil :type (or null function))
  (heading-counters (make-array 10 :initial-element 0) :type vector)
  (toc nil :type list)
  (readtable-stack nil :type list))

(defun make-empty-context ()
  "Create a new empty lun context."
  (make-lun-context))

;;; ============================================================================
;;; CACHE PERSISTENCE
;;; ============================================================================

(defun cache-file-path (root-directory)
  "Get the path to the cache file for a directory."
  (merge-pathnames +cache-filename+ 
                   (uiop:ensure-directory-pathname root-directory)))

(defun save-cache-to-disk (cache root-directory)
  "Save metadata cache to disk as a readable lisp expression."
  (let ((cache-path (cache-file-path root-directory)))
    (with-open-file (out cache-path
                         :direction :output
                         :if-exists :supersede
                         :if-does-not-exist :create)
      (format out ";;; LUN2 Metadata Cache~%")
      (format out ";;; Generated: ~A~%~%" (multiple-value-bind (sec min hr day mon yr)
                                             (get-decoded-time)
                                           (format nil "~4,'0D-~2,'0D-~2,'0D ~2,'0D:~2,'0D:~2,'0D"
                                                   yr mon day hr min sec)))
      (format out "(~%")
      (maphash (lambda (key value)
                 (format out "  (~S~%   ~S)~%" key value))
               cache)
      (format out ")~%"))))

(defun load-cache-from-disk (root-directory)
  "Load metadata cache from disk into a hash table."
  (let ((cache-path (cache-file-path root-directory))
        (cache (make-hash-table :test #'equal)))
    (when (probe-file cache-path)
      (with-open-file (in cache-path :direction :input)
        (let ((data (read in nil nil)))
          (when (listp data)
            (dolist (entry data)
              (when (and (listp entry) (= (length entry) 2))
                (setf (gethash (first entry) cache) (second entry))))))))
    cache))

(defun update-cache-entry (cache key value)
  "Return new cache with updated entry."
  (let ((new-cache (make-hash-table :test #'equal)))
    (maphash (lambda (k v) (setf (gethash k new-cache) v)) cache)
    (setf (gethash key new-cache) value)
    new-cache))

;;; ============================================================================
;;; CONTEXT OPERATIONS
;;; ============================================================================

(defun with-root-directory (context directory)
  "Return context with root directory set."
  (let ((new-ctx (copy-lun-context context)))
    (setf (lun-context-root-directory new-ctx) 
          (uiop:ensure-directory-pathname directory))
    new-ctx))

(defun with-current-file (context file-path)
  "Return context with current file set."
  (let ((new-ctx (copy-lun-context context)))
    (setf (lun-context-current-file new-ctx) (pathname file-path))
    new-ctx))

(defun with-template (context template-fn)
  "Return context with template function set."
  (let ((new-ctx (copy-lun-context context)))
    (setf (lun-context-site-template new-ctx) template-fn)
    new-ctx))

(defun with-reset-counters (context)
  "Return context with reset heading counters."
  (let ((new-ctx (copy-lun-context context)))
    (setf (lun-context-heading-counters new-ctx) 
          (make-array 10 :initial-element 0))
    (setf (lun-context-toc new-ctx) nil)
    new-ctx))

(defun with-metadata-entry (context key value)
  "Return context with updated metadata cache entry."
  (let ((new-ctx (copy-lun-context context)))
    (setf (lun-context-metadata-cache new-ctx)
          (update-cache-entry (lun-context-metadata-cache context) key value))
    new-ctx))

(defun with-toc-entry (context level number text id)
  "Return context with TOC entry added."
  (let ((new-ctx (copy-lun-context context)))
    (setf (lun-context-toc new-ctx)
          (cons (list :level level :number number :text text :id id)
                (lun-context-toc context)))
    new-ctx))

(defun with-incremented-heading (context level)
  "Return context with heading counter incremented at level."
  (let* ((new-ctx (copy-lun-context context))
         (counters (copy-seq (lun-context-heading-counters context))))
    (incf (aref counters (1- level)))
    (loop for i from level below (length counters)
          do (setf (aref counters i) 0))
    (setf (lun-context-heading-counters new-ctx) counters)
    new-ctx))

(defun load-cache-into-context (context)
  "Return context with cache loaded from disk."
  (let* ((root-dir (lun-context-root-directory context))
         (cache (if root-dir
                    (load-cache-from-disk root-dir)
                    (make-hash-table :test #'equal)))
         (new-ctx (copy-lun-context context)))
    (setf (lun-context-metadata-cache new-ctx) cache)
    new-ctx))

(defun save-context-cache (context)
  "Save context's cache to disk."
  (let ((root-dir (lun-context-root-directory context)))
    (when root-dir
      (save-cache-to-disk (lun-context-metadata-cache context) root-dir)))
  context)

;;; ============================================================================
;;; HEADING NUMBERING
;;; ============================================================================

(defun build-heading-number (counters level)
  "Build the number string (e.g., '1.2.3') for given level."
  (with-output-to-string (s)
    (loop for i from 0 below level
          for count = (aref counters i)
          when (plusp count)
          do (unless (zerop i) (write-char #\. s))
             (format s "~A" count))))

(defun get-current-number (context level)
  "Get the current heading number without incrementing."
  (build-heading-number (lun-context-heading-counters context) level))

(defun get-and-increment-heading (context level)
  "Get heading number and return updated context."
  (let ((new-ctx (with-incremented-heading context level)))
    (values (build-heading-number (lun-context-heading-counters new-ctx) level)
            new-ctx)))

;;; ============================================================================
;;; HTML UTILITIES
;;; ============================================================================

(defun make-heading-id (text)
  "Convert heading text to a valid HTML id: lowercase, no special chars."
  (with-output-to-string (s)
    (loop for char across (string-downcase text)
          when (or (alphanumericp char) (char= char #\-))
          do (write-char char s))))

(defun format-heading-html (level number text id)
  "Format heading as HTML with anchor and number."
  (format nil "<span id=\"~A\"></span>~%<h~A><a href=\"#~A\">~A ~A</a></h~A>~%" 
          id level id number text level))

(defun heading-with-context (context level text)
  "Generate heading HTML and return updated context."
  (multiple-value-bind (number new-ctx)
      (get-and-increment-heading context level)
    (let* ((id (make-heading-id text))
           (html (format-heading-html level number text id))
           (final-ctx (with-toc-entry new-ctx level number text id)))
      (values html final-ctx))))

(defun bold (text)
  "Formats a given text as strong html."
  (format nil "<strong>~A</strong>" text))

(defun link (text target)
  "Format some given text and target as a html link."
  (format nil "<a href=\"~A\">~A</a>" target text))

(defun li (text)
  "Format text as a list item."
  (format nil "<li>~A</li>" text))

;;; ============================================================================
;;; READER MACROS (with dynamic context via special variable)
;;; ============================================================================

(defvar *processing-context* nil
  "Dynamic context during reader macro evaluation.")

(defun count-leading-chars (stream char)
  "Count consecutive occurrences of char at current stream position."
  (let ((count 1))
    (loop while (eql (peek-char nil stream nil :eof) char)
          do (read-char stream)
             (incf count))
    count))

(defun read-until-char (stream end-char)
  "Read characters until end-char is encountered."
  (with-output-to-string (s)
    (loop for c = (read-char stream nil :eof)
          until (or (eql c :eof) (eql c end-char))
          do (write-char c s))))

(defun read-heading (stream char)
  "Reader macro for # - reads rest of line and returns heading form."
  (declare (ignore char))
  (let* ((level (count-leading-chars stream #\#))
         (text (string-trim '(#\Space #\Tab) (read-line stream nil ""))))
    `(concatenate 'string
                  (heading-text ,level ,text)
                  ,(string #\Newline))))

(defun read-bold (stream char)
  "Reader macro for * - reads until closing * and returns bold form."
  (declare (ignore char))
  (let ((text (read-until-char stream #\*)))
    `(bold ,text)))

(defun read-link (stream char)
  "Reader macro for [ - reads [text](url) and returns link form."
  (declare (ignore char))
  (let ((text (read-until-char stream #\])))
    (let ((next-char (read-char stream nil :eof)))
      (if (eql next-char #\()
          (let ((url (read-until-char stream #\))))
            `(link ,text ,url))
          (concatenate 'string "[" text (string next-char))))))

;;; ============================================================================
;;; READTABLE MANAGEMENT (functional style)
;;; ============================================================================

(defun push-readtable (context)
  "Return context with current readtable pushed onto stack."
  (let ((new-ctx (copy-lun-context context)))
    (setf (lun-context-readtable-stack new-ctx)
          (cons *readtable* (lun-context-readtable-stack context)))
    new-ctx))

(defun pop-readtable (context)
  "Return context with readtable popped and restored."
  (let ((new-ctx (copy-lun-context context))
        (stack (lun-context-readtable-stack context)))
    (when stack
      (setf *readtable* (car stack))
      (setf (lun-context-readtable-stack new-ctx) (cdr stack)))
    new-ctx))

(defun enable-lun-syntax (context)
  "Enable lun syntax and return updated context."
  (let ((new-ctx (push-readtable context)))
    (setq *readtable* (copy-readtable))
    (set-macro-character +hashtag+ 'read-heading)
    (set-macro-character #\* 'read-bold nil)
    (set-macro-character #\[ 'read-link)
    new-ctx))

(defun disable-lun-syntax (context)
  "Restore previous readtable and return updated context."
  (pop-readtable context))

;;; ============================================================================
;;; PARSING
;;; ============================================================================

(defun special-char-p (char)
  "Check if char triggers reader macro."
  (or (eql char #\#) (eql char #\*) (eql char #\[) (eql char #\()))

(defun read-raw-text-chunk (stream)
  "Reads characters until EOF or special character."
  (with-output-to-string (s)
    (loop for c = (peek-char nil stream nil :eof)
          until (or (eql c :eof) (special-char-p c))
          do (write-char (read-char stream) s))))

(defun read-lun-content (stream)
  "Parse stream into list of strings and forms."
  (loop with forms = nil
        for c = (peek-char nil stream nil :eof)
        until (eql c :eof)
        do (if (special-char-p c)
               (let ((form (read-preserving-whitespace stream nil :eof)))
                 (unless (eql form :eof)
                   (push form forms)))
               (let ((text (read-raw-text-chunk stream)))
                 (when (plusp (length text))
                   (push text forms))))
        finally (return (nreverse forms))))

;;; ============================================================================
;;; FORM EVALUATION WITH CONTEXT
;;; ============================================================================

(defun heading-text (level text)
  "Evaluate heading with dynamic context."
  (multiple-value-bind (html new-ctx)
      (heading-with-context *processing-context* level text)
    (setf *processing-context* new-ctx)
    html))

(defun meta-with-context (context &rest plist)
  "Store metadata and return updated context."
  (let ((file (lun-context-current-file context)))
    (values "" (with-metadata-entry context file plist))))

(defmacro meta (&rest plist)
  "Declare metadata for current file. Returns empty string."
  `(progn
     (multiple-value-bind (result new-ctx)
         (meta-with-context *processing-context* ,@plist)
       (setf *processing-context* new-ctx)
       result)))

;;; ============================================================================
;;; OUTPUT GENERATION
;;; ============================================================================

(defun non-whitespace-p (text)
  "Check if text contains non-whitespace characters."
  (and (> (length text) 0)
       (not (every (lambda (c) (member c '(#\Space #\Tab #\Newline))) text))))

(defun flush-paragraph (text-buffer stream)
  "Write accumulated text as a paragraph."
  (when text-buffer
    (let ((combined (apply #'concatenate 'string (nreverse text-buffer))))
      (when (non-whitespace-p combined)
        (format stream "<p>~A</p>~%~%" (string-trim '(#\Newline) combined))))))

(defun write-code-item (item stream)
  "Evaluate and write a code form."
  (let ((result (eval item)))
    (when (and result
               (or (not (stringp result))
                   (non-whitespace-p (string-trim '(#\Space #\Tab #\Newline) result))))
      (format stream "~A" result))))

(defun write-lun-output (parsed-content &optional (stream *standard-output*))
  "Write parsed content to stream, wrapping consecutive text in <p> tags."
  (loop with last-was-code = nil
        with text-buffer = nil
        for item in parsed-content
        for is-code = (not (stringp item))
        do (if is-code
               (progn
                 (flush-paragraph text-buffer stream)
                 (setf text-buffer nil)
                 (write-code-item item stream)
                 (setf last-was-code t))
               (progn
                 (when (and last-was-code
                           (plusp (length item))
                           (not (member (char item 0) '(#\Space #\Tab #\Newline))))
                   (format stream "~%"))
                 (push item text-buffer)
                 (setf last-was-code nil)))
        finally (flush-paragraph text-buffer stream)))

;;; ============================================================================
;;; FILE PROCESSING
;;; ============================================================================

(defun initialize-file-context (context file-path)
  "Return context initialized for processing a file."
  (let* ((ctx (with-current-file context file-path))
         (ctx (with-reset-counters ctx)))
    (unless (lun-context-root-directory ctx)
      (setf ctx (with-root-directory ctx (uiop:pathname-directory-pathname file-path))))
    ctx))

(defun parse-lun-file (context file-path)
  "Parse a lun file and return forms and updated context."
  (let ((ctx (enable-lun-syntax context)))
    (unwind-protect
         (with-open-file (stream file-path :direction :input)
           (values (read-lun-content stream) ctx))
      (disable-lun-syntax ctx))))

(defun store-toc (context)
  "Store table of contents in cache and return updated context."
  (let* ((file (lun-context-current-file context))
         (toc (reverse (lun-context-toc context)))
         (key (cons file :toc)))
    (with-metadata-entry context key toc)))

(defun apply-template (context content)
  "Apply site template if configured."
  (let ((template (lun-context-site-template context)))
    (if template
        (let* ((file (lun-context-current-file context))
               (cache (lun-context-metadata-cache context))
               (metadata (gethash file cache)))
          (funcall template content metadata))
        content)))

(defun process-lun-file (context file-path &key (output-stream *standard-output*))
  "Process a lun file with context, return updated context."
  (let ((ctx (initialize-file-context context file-path)))
    (multiple-value-bind (parsed-content ctx)
        (parse-lun-file ctx file-path)
      (let ((*processing-context* ctx))
        (let* ((content (with-output-to-string (s)
                          (write-lun-output parsed-content s)))
               (final-ctx *processing-context*)
               (wrapped (apply-template final-ctx content)))
          (write-string wrapped output-stream)
          (store-toc final-ctx))))))

(defun lun-file-to-string (context file-path)
  "Process .lun file and return result as string and updated context."
  (with-output-to-string (s)
    (process-lun-file context file-path :output-stream s)))

(defun lun-file-to-file (context input-path output-path)
  "Process .lun file and write result to output file, return updated context."
  (with-open-file (out output-path 
                       :direction :output 
                       :if-exists :supersede 
                       :if-does-not-exist :create)
    (process-lun-file context input-path :output-stream out)))

;;; ============================================================================
;;; METADATA OPERATIONS
;;; ============================================================================

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

(defun load-file-metadata (context file-path)
  "Load metadata from first form of file, return updated context."
  (let ((cache (lun-context-metadata-cache context)))
    (if (gethash file-path cache)
        context
        (let ((ctx (enable-lun-syntax context)))
          (unwind-protect
               (with-open-file (stream file-path :direction :input)
                 (let* ((ctx (with-current-file ctx file-path))
                        (*processing-context* ctx))
                   (let ((first-form (read stream nil :eof)))
                     (when (and (consp first-form) (eq (car first-form) 'meta))
                       (eval first-form))
                     (let ((final-ctx *processing-context*))
                       (unless (gethash file-path (lun-context-metadata-cache final-ctx))
                         (with-metadata-entry final-ctx file-path nil))
                       final-ctx))))
            (disable-lun-syntax ctx))))))

(defun scan-directory-metadata (context directory)
  "Scan all .lun files in directory and load their metadata."
  (let ((ctx (with-root-directory context directory)))
    (reduce #'load-file-metadata
            (find-lun-files directory)
            :initial-value ctx)))

(defun get-metadata (context file-path key &optional default)
  "Get metadata value for a file."
  (let* ((ctx (load-file-metadata context file-path))
         (cache (lun-context-metadata-cache ctx))
         (metadata (gethash file-path cache)))
    (values (getf metadata key default) ctx)))

(defun has-tag-p (context file-path tag)
  "Check if file has a specific tag."
  (multiple-value-bind (tags ctx)
      (get-metadata context file-path :tags)
    (values (member tag tags) ctx)))

;;; ============================================================================
;;; QUERYING
;;; ============================================================================

(defun matches-tags-p (file-tags required-tags)
  "Check if all required tags are present."
  (every (lambda (tag) (member tag file-tags)) required-tags))

(defun matches-any-tag-p (file-tags any-tags)
  "Check if any of the tags are present."
  (some (lambda (tag) (member tag file-tags)) any-tags))

(defun matches-title-p (title substring)
  "Check if title contains substring."
  (search substring title :test #'char-equal))

(defun file-matches-criteria-p (metadata &key tags any-tag title-contains)
  "Check if file metadata matches all criteria."
  (let ((file-tags (getf metadata :tags))
        (file-title (getf metadata :title "")))
    (and (or (not tags) (matches-tags-p file-tags tags))
         (or (not any-tag) (matches-any-tag-p file-tags any-tag))
         (or (not title-contains) (matches-title-p file-title title-contains)))))

(defun make-sort-comparator (sort-by)
  "Create comparison function for sorting."
  (lambda (a b)
    (let ((meta-a (getf a :metadata))
          (meta-b (getf b :metadata)))
      (cond
        ((eq sort-by :date)
         (string< (getf meta-a :date "") (getf meta-b :date "")))
        ((eq sort-by :title)
         (string< (getf meta-a :title "") (getf meta-b :title "")))
        (t nil)))))

(defun query-files (context &key tags any-tag title-contains sort-by)
  "Query files based on metadata criteria."
  (let ((results nil)
        (cache (lun-context-metadata-cache context)))
    (maphash (lambda (file-path metadata)
               (when (and (pathnamep file-path)
                         (file-matches-criteria-p metadata 
                                                  :tags tags 
                                                  :any-tag any-tag 
                                                  :title-contains title-contains))
                 (push (list :file file-path :metadata metadata) results)))
             cache)
    (when sort-by
      (setf results (sort results (make-sort-comparator sort-by))))
    (nreverse results)))

;;; ============================================================================
;;; RENDERING
;;; ============================================================================

(defun file-to-html-path (lun-file-path)
  "Convert .lun file path to .html output path."
  (make-pathname :type "html" :defaults lun-file-path))

(defun relative-link (from-file to-file)
  "Generate relative link from one file to another."
  (let* ((from-html (file-to-html-path from-file))
         (to-html (file-to-html-path to-file))
         (from-dir (uiop:pathname-directory-pathname from-html)))
    (enough-namestring to-html from-dir)))

(defun render-file-link (context file-path &key (link-text nil))
  "Render HTML link to another lun file."
  (let* ((current-file (lun-context-current-file context))
         (cache (lun-context-metadata-cache context))
         (metadata (gethash file-path cache))
         (title (or link-text 
                   (getf metadata :title)
                   (pathname-name file-path)))
         (relative-url (relative-link current-file file-path)))
    (format nil "<a href=\"~A\">~A</a>" relative-url title)))

(defun render-file-list-item (context file-info show-date show-tags show-description)
  "Render a single file list item."
  (let* ((file-path (getf file-info :file))
         (metadata (getf file-info :metadata))
         (title (getf metadata :title (pathname-name file-path)))
         (date (getf metadata :date))
         (tags (getf metadata :tags))
         (description (getf metadata :description)))
    (with-output-to-string (s)
      (format s "  <li>~%")
      (format s "    ~A" (render-file-link context file-path :link-text title))
      (when (and show-date date)
        (format s " <span class=\"date\">(~A)</span>" date))
      (when (and show-tags tags)
        (format s " <span class=\"tags\">[~{~A~^, ~}]</span>" tags))
      (when (and show-description description)
        (format s "<br>~%    <span class=\"description\">~A</span>" description))
      (format s "~%  </li>~%"))))

(defun render-file-list (context files &key (show-date t) (show-tags nil) (show-description t))
  "Render a list of files as HTML."
  (with-output-to-string (s)
    (format s "<ul class=\"file-list\">~%")
    (dolist (file-info files)
      (format s "~A" (render-file-list-item context file-info show-date show-tags show-description)))
    (format s "</ul>~%")))

(defun query-and-render (context &key tags any-tag title-contains sort-by 
                                      (show-date t) (show-tags nil) (show-description t))
  "Query files and render as HTML list."
  (let ((results (query-files context
                              :tags tags 
                              :any-tag any-tag
                              :title-contains title-contains
                              :sort-by sort-by)))
    (render-file-list context results 
                     :show-date show-date
                     :show-tags show-tags
                     :show-description show-description)))

(defmacro query-and-list (&key tags any-tag title-contains sort-by 
                               (show-date t) (show-tags nil) (show-description t))
  "Query files and render as HTML list using dynamic context."
  `(query-and-render *processing-context*
                     :tags ',tags 
                     :any-tag ',any-tag
                     :title-contains ,title-contains
                     :sort-by ,sort-by
                     :show-date ,show-date
                     :show-tags ,show-tags
                     :show-description ,show-description))

;;; ============================================================================
;;; TABLE OF CONTENTS
;;; ============================================================================

(defun render-toc-item (entry prev-level)
  "Render a single TOC entry, managing nesting."
  (let ((level (getf entry :level))
        (number (getf entry :number))
        (text (getf entry :text))
        (id (getf entry :id)))
    (with-output-to-string (s)
      (cond
        ((> level prev-level) (format s "~vt<ul>~%" (1- level)))
        ((< level prev-level) (format s "~vt</ul>~%" (1+ level))))
      (format s "~vt<li><a href=\"#~A\">~A ~A</a></li>~%" level id number text))))

(defun table-of-contents-from-context (context)
  "Generate the table of contents for the current file."
  (let* ((file (lun-context-current-file context))
         (cache (lun-context-metadata-cache context))
         (toc (gethash (cons file :toc) cache)))
    (if toc
        (with-output-to-string (s)
          (format s "<nav class=\"toc\">~%~t<h3>Table Of Contents</h3>~%~t<ul class=\"toc\">~%")
          (loop with prev-level = 1
                for entry in toc
                do (format s "~A" (render-toc-item entry prev-level))
                   (setf prev-level (getf entry :level)))
          (format s "~t</ul>~%</nav>~%~%"))
        "")))

(defun table-of-contents ()
  "Generate TOC using dynamic context."
  (table-of-contents-from-context *processing-context*))

;;; ============================================================================
;;; BACKLINKS
;;; ============================================================================

(defun substitute-string (string old new)
  "Replace all occurrences of OLD with NEW in STRING."
  (let ((pos (search old string)))
    (if pos
        (concatenate 'string
                     (subseq string 0 pos)
                     new
                     (substitute-string (subseq string (+ pos (length old))) old new))
        string)))

(defun extract-target-from-form (form)
  "Safely extract target path from a link form."
  (cond
    ;; Case 1: (insert-file-link "path" ...) - DSL Macro
    ((eq (car form) 'insert-file-link)
     (second form))
    
    ;; Case 2: (render-file-link context "path" ...) - Internal Function
    ((eq (car form) 'render-file-link)
     (third form))
    
    (t nil)))

(defun resolve-link-target (target file-dir)
  "Resolve a target string/path to a full pathname."
  (when (stringp target)
    (cond
      ((search ".lun" target)
       (merge-pathnames target file-dir))
      ((search ".html" target)
       (let ((lun-url (substitute-string target ".html" ".lun")))
         (merge-pathnames lun-url file-dir)))
      (t nil))))

(defun extract-links-from-forms (forms file-dir)
  "Recursively walk forms to find links."
  (let ((links nil))
    (labels ((walker (form)
               (cond
                 ((consp form)
                  ;; 1. Check for function/macro calls (insert-file-link or render-file-link)
                  (let ((target-raw (extract-target-from-form form)))
                    ;; If we found a target, try to resolve it. 
                    ;; We avoid EVAL here to prevent UNBOUND-VARIABLE errors.
                    (when target-raw
                      (let ((resolved (resolve-link-target target-raw file-dir)))
                        (when (and resolved (probe-file resolved))
                          (push (truename resolved) links)))))

                  ;; 2. Check for standard markdown links [text](url) -> (link "text" "url")
                  (when (and (eq (car form) 'link)
                             (stringp (third form)))
                    (let ((resolved (resolve-link-target (third form) file-dir)))
                      (when (and resolved (probe-file resolved))
                         (push (truename resolved) links))))

                  ;; 3. Recurse into subforms
                  (dolist (sub form)
                    (walker sub)))
                 (t nil))))
      (dolist (f forms)
        (walker f)))
    ;; Return unique links
    (remove-duplicates links :test #'equal)))

(defun get-file-links (context file-path)
  "Parse a file solely to extract outgoing links."
  (let ((file-dir (uiop:pathname-directory-pathname file-path))
        ;; We use a temporary context just for parsing
        (temp-ctx (initialize-file-context context file-path)))
    (multiple-value-bind (forms _)
        (parse-lun-file temp-ctx file-path)
      (declare (ignore _))
      (extract-links-from-forms forms file-dir))))

(defun find-backlinks (context file-path)
  "Find all files that link to the given file-path."
  (let ((backlinks nil)
        (target-path (truename file-path))
        (cache (lun-context-metadata-cache context)))
    (maphash (lambda (key value)
               (declare (ignore value))
               (when (pathnamep key)
                 (let ((links (get-file-links context key)))
                   (when (member target-path links :test #'equal)
                     (push key backlinks)))))
             cache)
    backlinks))

(defun backlinks-render (context)
  "Generate backlinks HTML section for the current file."
  (let* ((current-file (lun-context-current-file context))
         (links (find-backlinks context current-file)))
    (if links
        (with-output-to-string (s)
          (format s "<nav class=\"backlinks\">~%<h3>Backlinks</h3>~%<ul>~%")
          (dolist (link-file links)
            (let* ((metadata (gethash link-file (lun-context-metadata-cache context)))
                   (title (getf metadata :title (pathname-name link-file)))
                   (relative-url (relative-link current-file link-file)))
              (format s "<li><a href=\"~A\">~A</a></li>~%" relative-url title)))
          (format s "</ul>~%</nav>~%~%"))
        "")))

;;; ============================================================================
;;; DSL WRAPPERS
;;; These macros/functions allow the code inside .lun files to use the 
;;; *processing-context* implicitly.
;;; ============================================================================

(defmacro insert-file-link (path &key link-text)
  "Wrapper for render-file-link to be used inside .lun files."
  `(render-file-link *processing-context* ,path :link-text ,link-text))

(defun backlinks ()
  "Wrapper to render backlinks using the current context."
  (backlinks-render *processing-context*))

;;; ============================================================================
;;; SITE BUILDING
;;; ============================================================================

(defun build-site (input-directory output-directory &key (template nil))
  "Build entire site: scan metadata, save cache, process all files."
  (let* ((root-path (uiop:ensure-directory-pathname input-directory))
         (out-path (uiop:ensure-directory-pathname output-directory))
         (ctx (make-empty-context)))
    
    (format t "Initializing context...~%")
    (setf ctx (with-root-directory ctx root-path))
    (when template
      (setf ctx (with-template ctx template)))

    (format t "Pass 1: Scanning metadata...~%")
    ;; Scan and load all metadata into the context
    (setf ctx (scan-directory-metadata ctx root-path))
    
    ;; Persist the cache immediately
    (save-context-cache ctx)
    (format t "Metadata cache saved. Found ~A files.~%" 
            (hash-table-count (lun-context-metadata-cache ctx)))
    
    (format t "Pass 2: Processing and Rendering files...~%")
    (maphash (lambda (file-path metadata)
               (declare (ignore metadata))
               (format t "~A~%" file-path)
               (when (pathnamep file-path)
                 ;; Determine output path
                 (let* ((rel-path (enough-namestring file-path root-path))
                        (output-file (merge-pathnames 
                                      (make-pathname :type "html" :defaults rel-path)
                                      out-path)))
                   
                   (ensure-directories-exist output-file)
                   (format t "  ~A -> ~A~%" rel-path (enough-namestring output-file out-path))
                   
                   ;; Process the file (this writes HTML and updates TOC in context, 
                   ;; though we don't strictly need the TOC update for subsequent files
                   ;; in this pass, the cache persistence handles it for next run)
                   (process-lun-file ctx file-path 
                                   :output-stream (open output-file 
                                                        :direction :output 
                                                        :if-exists :supersede
                                                        :if-does-not-exist :create)))))
             (lun-context-metadata-cache ctx))
    
    (format t "Build complete.~%")))
