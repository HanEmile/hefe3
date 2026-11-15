---
title: Inventory
type: page
visibility: public
---

# Inventory

(toc)

## Shelves

(table :type "shelf" :columns (:title :location))

## All Boxes

(group-by :type "box" :by :shelf :show (:title :contains))

## Items

(listing :type "contains" :show (:title))
