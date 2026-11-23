import { useId, useState } from 'react';
import { ExternalLink, Search } from 'lucide-react';
import { BASE_DOMAIN, shopLoginUrl } from '@/lib/config';
import { slugify } from '@/lib/format';

/** Type a shop name, get the shop's own login address. */
export function FindShop() {
  const id = useId();
  const [name, setName] = useState('');
  const slug = slugify(name);

  return (
    <section id="find-shop" tabIndex={-1} aria-labelledby={`${id}-t`} className="scroll-mt-6 rounded-3xl bg-ink p-6 text-slate-200 outline-none sm:p-8">
      <h2 id={`${id}-t`} className="flex items-center gap-2.5 text-2xl font-extrabold tracking-[-0.02em] text-white">
        <Search className="size-[22px] text-brand-bright" aria-hidden="true" />
        Find my shop
      </h2>
      <p className="mt-2 mb-5 text-[15px] leading-relaxed text-slate-300">
        Already with MintoBill? Type your shop name to open your login page.
      </p>
      <label htmlFor={`${id}-in`} className="mb-1.5 block text-sm font-semibold text-slate-200">
        Shop name
      </label>
      <input
        id={`${id}-in`}
        type="text"
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="e.g. Tejas Shop"
        autoComplete="off"
        className="h-12 w-full rounded-[10px] border border-slate-700 bg-slate-800 px-3.5 text-[15px] text-white placeholder:text-slate-500"
      />
      <div className="mt-4 rounded-[14px] border border-slate-700 bg-slate-800 px-4 py-3.5">
        <p className="text-xs font-bold tracking-[0.08em] text-slate-400 uppercase">Your login address</p>
        <p className="mt-1.5 font-bold break-all text-blue-300" aria-live="polite">
          {slug || 'yourshop'}.{BASE_DOMAIN}/login
        </p>
      </div>
      {slug && (
        <a
          href={shopLoginUrl(slug)}
          className="mt-3.5 flex min-h-12 items-center justify-center gap-2 rounded-xl bg-brand text-[15px] font-bold text-white transition-colors hover:bg-brand-strong"
        >
          Go to my shop
          <ExternalLink className="size-4" aria-hidden="true" />
        </a>
      )}
      <p className="mt-3 text-[13px] text-slate-400">
        Forgot your address?{' '}
        <a href="/find-my-shop" className="font-semibold text-blue-300 underline underline-offset-2 hover:text-white">
          Get it by email
        </a>
      </p>
    </section>
  );
}
