import React, { Suspense, lazy, useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes, useParams } from 'react-router-dom';

// The tenant app loads in two separate parts: the public shop (/,
// /product/..., /shop/...) and the staff app (sign-in and back office,
// every other path). Shop visitors never download the staff code.
const StaffApp = lazy(() => import('./App'));
const PublicShopPage = lazy(() => import('./pages/PublicShopPage'));

const LOGIN = { homePage: 'login' };

// useHome asks what the main address shows (GET /api/public/home):
// { homePage: 'shop', slug } or { homePage: 'login' }. Errors count as login.
function useHome() {
  const [home, setHome] = useState(null);
  useEffect(() => {
    let live = true;
    fetch('/api/public/home')
      .then((r) => (r.ok ? r.json() : LOGIN))
      .catch(() => LOGIN)
      .then((h) => {
        if (live) setHome(h?.homePage === 'shop' && typeof h.slug === 'string' && h.slug ? h : LOGIN);
      });
    return () => {
      live = false;
    };
  }, []);
  return home;
}

function Blank() {
  return <div className="min-h-screen" />;
}

// Home is / and /product/:productId: the chosen store's shop, or the staff
// sign-in when the tenant shows no shop there.
function Home() {
  const home = useHome();
  if (!home) return <Blank />;
  if (home.homePage !== 'shop') return <Navigate to="/login" replace />;
  return <PublicShopPage slug={home.slug} />;
}

// ShopBySlug is /shop/:slug. The store shown at / moves there, so old
// links keep working; other stores stay at /shop/:slug.
function ShopBySlug() {
  const { slug, productId } = useParams();
  const home = useHome();
  if (!home) return <Blank />;
  if (home.homePage === 'shop' && home.slug.toLowerCase() === slug.toLowerCase()) {
    return <Navigate to={productId ? `/product/${encodeURIComponent(productId)}` : '/'} replace />;
  }
  return <PublicShopPage slug={slug} />;
}

export default function Root() {
  return (
    <BrowserRouter>
      <Suspense fallback={<Blank />}>
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/product/:productId" element={<Home />} />
          <Route path="/shop/:slug" element={<ShopBySlug />} />
          <Route path="/shop/:slug/product/:productId" element={<ShopBySlug />} />
          <Route path="*" element={<StaffApp />} />
        </Routes>
      </Suspense>
    </BrowserRouter>
  );
}
