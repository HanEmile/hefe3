---
title: Functional Programming Patterns
date: 2025-12-05
author: Emile
tags: functional-programming, patterns
---

# Functional Programming Patterns

Let me share some elegant functional programming patterns I've discovered while working with Lun.

## Map-Filter-Reduce

The holy trinity of functional programming:

```lisp
(define sum-of-squares 
  (lambda (lst)
    (fold + 0 (map square (filter even? lst)))))
```

## Recursion Over Iteration

In Lun, everything is recursion:

```lisp
(define factorial 
  (lambda (n)
    (if (< n 2) 
        1 
        (* n (factorial (- n 1))))))
```

## Higher-Order Functions

Functions that return functions are powerful:

```lisp
(define make-adder 
  (lambda (x) 
    (lambda (y) (+ x y))))
```

These patterns make code elegant and composable!
