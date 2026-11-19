import { useEffect } from 'react';

/** Sets the document title and meta description for the current page. */
export function usePageMeta(title, description) {
  useEffect(() => {
    document.title = title;
    if (description) {
      document.querySelector('meta[name="description"]')?.setAttribute('content', description);
    }
  }, [title, description]);
}
