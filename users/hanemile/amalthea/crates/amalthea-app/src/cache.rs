/// Generic LRU (Least Recently Used) cache.
///
/// Stores up to `capacity` entries. When an insertion would exceed
/// capacity the least-recently-used entry is evicted.
///
/// Internally a doubly-linked list of nodes living in a `Vec` arena
/// gives every operation (`insert`, `get`, `remove`) constant time.
/// Removed slots are recycled via a free-list so the arena never grows
/// past `capacity`.
///
/// No external dependencies — intentionally simple and correct.
///
/// Last reviewed: 2026-05-01 | Review frequency: monthly

use std::collections::HashMap;
use std::hash::Hash;

/// Sentinel for "no neighbour" in the linked list.
const NIL: usize = usize::MAX;

/// One node in the LRU's doubly-linked list. Lives inside a `Vec`
/// arena; `prev` / `next` are indices into the same arena.
struct Node<K, V> {
    key: K,
    value: V,
    prev: usize,
    next: usize,
}

/// A generic LRU cache with a fixed maximum number of entries. All
/// operations run in O(1) amortised time.
pub struct LruCache<K, V> {
    capacity: usize,
    /// Key → arena index.
    map: HashMap<K, usize>,
    /// Node arena. Slots are reused: when a node is removed its index
    /// is pushed to `free` and the storage is reclaimed by the next
    /// insert.
    arena: Vec<Option<Node<K, V>>>,
    free: Vec<usize>,
    /// Most-recently-used end (front of the list).
    head: usize,
    /// Least-recently-used end (back of the list).
    tail: usize,
}

impl<K, V> LruCache<K, V>
where
    K: Eq + Hash + Clone,
{
    /// Create a new LRU cache with the given maximum capacity.
    ///
    /// # Panics
    ///
    /// Panics if `capacity` is zero.
    pub fn new(capacity: usize) -> Self {
        assert!(capacity > 0, "LruCache capacity must be greater than zero");
        Self {
            capacity,
            map: HashMap::with_capacity(capacity),
            arena: Vec::with_capacity(capacity),
            free: Vec::new(),
            head: NIL,
            tail: NIL,
        }
    }

    /// Insert a key-value pair into the cache.
    ///
    /// If the key already exists its value is updated and the entry is
    /// promoted to most-recently-used. If the cache is full the
    /// least-recently-used entry is evicted first.
    ///
    /// Returns the previous value associated with the key, if any.
    pub fn insert(&mut self, key: K, value: V) -> Option<V> {
        if let Some(&idx) = self.map.get(&key) {
            let old = std::mem::replace(&mut self.node_mut(idx).value, value);
            self.move_to_front(idx);
            return Some(old);
        }

        if self.map.len() >= self.capacity {
            self.evict_lru();
        }

        let idx = self.alloc_node(Node {
            key: key.clone(),
            value,
            prev: NIL,
            next: NIL,
        });
        self.map.insert(key, idx);
        self.push_front(idx);
        None
    }

    /// Get a reference to the value for `key`, promoting it to
    /// most-recently-used.
    pub fn get(&mut self, key: &K) -> Option<&V> {
        let idx = *self.map.get(key)?;
        self.move_to_front(idx);
        Some(&self.node(idx).value)
    }

    /// Get a reference to the value for `key` without updating the
    /// access order.
    pub fn peek(&self, key: &K) -> Option<&V> {
        let idx = *self.map.get(key)?;
        Some(&self.node(idx).value)
    }

    /// Returns `true` if the cache contains the given key.
    pub fn contains(&self, key: &K) -> bool {
        self.map.contains_key(key)
    }

    /// Remove the entry for `key`, returning the value if it existed.
    pub fn remove(&mut self, key: &K) -> Option<V> {
        let idx = self.map.remove(key)?;
        self.unlink(idx);
        let node = self.arena[idx].take()?;
        self.free.push(idx);
        Some(node.value)
    }

    /// Number of entries currently in the cache.
    pub fn len(&self) -> usize {
        self.map.len()
    }

    /// Returns `true` if the cache is empty.
    pub fn is_empty(&self) -> bool {
        self.map.is_empty()
    }

    /// Remove all entries.
    pub fn clear(&mut self) {
        self.map.clear();
        self.arena.clear();
        self.free.clear();
        self.head = NIL;
        self.tail = NIL;
    }

    /// The maximum number of entries this cache can hold.
    pub fn capacity(&self) -> usize {
        self.capacity
    }

    // -- Linked list internals ------------------------------------------------

    fn node(&self, idx: usize) -> &Node<K, V> {
        self.arena[idx].as_ref().expect("dangling LRU index")
    }

    fn node_mut(&mut self, idx: usize) -> &mut Node<K, V> {
        self.arena[idx].as_mut().expect("dangling LRU index")
    }

    fn alloc_node(&mut self, node: Node<K, V>) -> usize {
        if let Some(idx) = self.free.pop() {
            self.arena[idx] = Some(node);
            idx
        } else {
            self.arena.push(Some(node));
            self.arena.len() - 1
        }
    }

    /// Detach `idx` from the list, but leave it in the arena.
    fn unlink(&mut self, idx: usize) {
        let prev = self.node(idx).prev;
        let next = self.node(idx).next;
        if prev != NIL {
            self.node_mut(prev).next = next;
        } else {
            self.head = next;
        }
        if next != NIL {
            self.node_mut(next).prev = prev;
        } else {
            self.tail = prev;
        }
        let n = self.node_mut(idx);
        n.prev = NIL;
        n.next = NIL;
    }

    /// Insert `idx` at the head (most-recently-used).
    fn push_front(&mut self, idx: usize) {
        let old_head = self.head;
        self.node_mut(idx).prev = NIL;
        self.node_mut(idx).next = old_head;
        if old_head != NIL {
            self.node_mut(old_head).prev = idx;
        }
        self.head = idx;
        if self.tail == NIL {
            self.tail = idx;
        }
    }

    fn move_to_front(&mut self, idx: usize) {
        if self.head == idx {
            return;
        }
        self.unlink(idx);
        self.push_front(idx);
    }

    fn evict_lru(&mut self) {
        let idx = self.tail;
        if idx == NIL {
            return;
        }
        self.unlink(idx);
        if let Some(node) = self.arena[idx].take() {
            self.map.remove(&node.key);
        }
        self.free.push(idx);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn evicts_least_recently_used() {
        let mut cache = LruCache::new(2);
        cache.insert("a", 1);
        cache.insert("b", 2);
        cache.insert("c", 3); // should evict "a"

        assert!(!cache.contains(&"a"));
        assert!(cache.contains(&"b"));
        assert!(cache.contains(&"c"));
        assert_eq!(cache.len(), 2);
    }

    #[test]
    fn get_promotes_entry() {
        let mut cache = LruCache::new(2);
        cache.insert("a", 1);
        cache.insert("b", 2);
        cache.get(&"a"); // promote "a", so "b" is now LRU
        cache.insert("c", 3); // should evict "b"

        assert!(cache.contains(&"a"));
        assert!(!cache.contains(&"b"));
        assert!(cache.contains(&"c"));
    }

    #[test]
    fn insert_existing_key_updates_value() {
        let mut cache = LruCache::new(2);
        cache.insert("a", 1);
        let old = cache.insert("a", 10);
        assert_eq!(old, Some(1));
        assert_eq!(cache.get(&"a"), Some(&10));
        assert_eq!(cache.len(), 1);
    }

    #[test]
    fn remove_works() {
        let mut cache = LruCache::new(3);
        cache.insert("a", 1);
        cache.insert("b", 2);
        assert_eq!(cache.remove(&"a"), Some(1));
        assert_eq!(cache.len(), 1);
        assert!(!cache.contains(&"a"));
    }

    #[test]
    fn clear_works() {
        let mut cache = LruCache::new(3);
        cache.insert("a", 1);
        cache.insert("b", 2);
        cache.clear();
        assert!(cache.is_empty());
        assert_eq!(cache.len(), 0);
    }

    #[test]
    #[should_panic(expected = "capacity must be greater than zero")]
    fn zero_capacity_panics() {
        let _cache: LruCache<String, i32> = LruCache::new(0);
    }

    #[test]
    fn churn_keeps_arena_bounded() {
        // Insert way more than capacity to make sure the free list is
        // recycling slots.
        let cap = 4;
        let mut cache = LruCache::new(cap);
        for i in 0..1000 {
            cache.insert(i, i * 2);
        }
        assert_eq!(cache.len(), cap);
        // Arena should never exceed capacity since slots are reused.
        assert!(cache.arena.len() <= cap);
    }

    #[test]
    fn promote_then_evict_picks_correct_lru() {
        let mut cache = LruCache::new(3);
        cache.insert("a", 1);
        cache.insert("b", 2);
        cache.insert("c", 3);
        // Order is now: c (head) -> b -> a (tail)
        cache.get(&"a"); // a is now MRU; b is LRU
        cache.insert("d", 4); // evicts b
        assert!(cache.contains(&"a"));
        assert!(!cache.contains(&"b"));
        assert!(cache.contains(&"c"));
        assert!(cache.contains(&"d"));
    }

    #[test]
    fn peek_does_not_promote() {
        let mut cache = LruCache::new(2);
        cache.insert("a", 1);
        cache.insert("b", 2);
        // peek should NOT change LRU order
        let _ = cache.peek(&"a");
        cache.insert("c", 3); // evicts "a" because peek didn't promote
        assert!(!cache.contains(&"a"));
        assert!(cache.contains(&"b"));
        assert!(cache.contains(&"c"));
    }
}
