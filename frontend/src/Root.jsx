import React, { Suspense, lazy, useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes, useLocation, useNavigate, useParams } from 'react-router-dom';

// The tenant app loads in two separate parts: the public shop (/,
// /product/..., /shop/...) and the staff app (sign-in and back office,
// every other path). Shop visitors never download the staff code.
const StaffApp = lazy(() => import('./App'));
const PublicShopPage = lazy(() => import('./pages/PublicShopPage'));

const LOGIN = { homePage: 'login' };

const isHome = (h) => h?.homePage === 'shop' && typeof h.slug === 'string' && h.slug;

// useHome asks what the main address shows (GET /api/public/home):
// { homePage: 'shop', slug } or { homePage: 'login' }. Errors count as login.
// known is an answer passed along by a redirect, so it is not asked twice.
function useHome(known = null) {
  const [home, setHome] = useState(known);
  useEffect(() => {
    if (known) return undefined;
    let live = true;
    fetch('/api/public/home')
      .then((r) => (r.ok ? r.json() : LOGIN))
      .catch(() => LOGIN)
      .then((h) => {
        if (live) setHome(isHome(h) ? h : LOGIN);
      });
    return () => {
      live = false;
    };
  }, [known]);
  return home;
}

function Blank() {
  return <div className="min-h-screen" />;
}

// Home is / and /product/:productId: the default store's shop, or the
// staff sign-in when the tenant shows no shop there.
function Home() {
  // Only a fresh answer: the browser keeps history state across reloads.
  const passed = useLocation().state;
  const fresh = isHome(passed?.home) && Date.now() - passed.at < 5000;
  const home = useHome(fresh ? passed.home : null);
  if (!home) return <Blank />;
  if (home.homePage !== 'shop') return <Navigate to="/login" replace />;
  return <PublicShopPage key={home.slug} slug={home.slug} homeSlug={home.slug} />;
}

// ShopBySlug is /shop/:slug. The default store moves to /, so old links
// keep working; other stores stay at /shop/:slug. A store that does not
// exist (or is inactive) is an unknown address: it goes to /.
function ShopBySlug() {
  const { slug, productId } = useParams();
  const navigate = useNavigate();
  const home = useHome();
  if (!home) return <Blank />;
  const homeSlug = home.homePage === 'shop' ? home.slug : null;
  if (homeSlug && homeSlug.toLowerCase() === slug.toLowerCase()) {
    return <Navigate to={productId ? `/product/${encodeURIComponent(productId)}` : '/'} state={{ home, at: Date.now() }} replace />;
  }
  return (
    <PublicShopPage key={slug} slug={slug} homeSlug={homeSlug} onMissing={() => navigate('/', { replace: true })} />
  );
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
