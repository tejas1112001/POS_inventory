import { useEffect, useState } from 'react';
import { api } from '@/services/api';

// Pictures stored privately (/api/files/{id}) need the signed-in token,
// which a plain <img src> cannot send. Public pictures load directly.
const cache = new Map(); // src -> Promise<object URL | null>

const isPrivate = (src) => typeof src === 'string' && src.startsWith('/api/files/');

function load(src) {
  if (!cache.has(src)) {
    cache.set(
      src,
      api
        .get(src, { raw: true, quiet: true })
        .then((res) => res.blob())
        .then((blob) => URL.createObjectURL(blob))
        .catch(() => {
          cache.delete(src); // try again next time
          return null;
        })
    );
  }
  return cache.get(src);
}

// useImageSrc returns a URL the browser can show, '' while loading, or
// null when the picture cannot be loaded.
export function useImageSrc(src) {
  const [url, setUrl] = useState(() => (isPrivate(src) ? '' : src || null));
  useEffect(() => {
    if (!isPrivate(src)) {
      setUrl(src || null);
      return undefined;
    }
    let live = true;
    setUrl('');
    load(src).then((u) => live && setUrl(u));
    return () => {
      live = false;
    };
  }, [src]);
  return url;
}

// AuthImage is an <img> that also works for private pictures. fallback is
// shown while loading and when the picture is missing.
export function AuthImage({ src, fallback = null, ...rest }) {
  const url = useImageSrc(src);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [url]);
  if (!url || failed) return fallback;
  return <img src={url} loading="lazy" decoding="async" onError={() => setFailed(true)} {...rest} />;
}
