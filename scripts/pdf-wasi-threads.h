#pragma once
// WASI preview1 worker is single threaded; OpenMP is disabled in its build.
namespace viii_serial {
struct mutex {
  void lock() {}
  void unlock() {}
};
template <typename M> struct lock_guard {
  explicit lock_guard(M&) {}
};
struct thread {
  thread() = default;
  template <typename F, typename... A> explicit thread(F&& f, A&&... a) {
    f(static_cast<A&&>(a)...);
  }
  void join() {}
  void detach() {}
  bool joinable() const { return false; }
};
namespace this_thread { inline void yield() {} }
}
