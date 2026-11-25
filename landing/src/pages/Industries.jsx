import { useSearchParams } from 'react-router-dom';
import { shopTypes, toolOrder } from '@/content/site';
import { useHydrated } from '@/lib/useHydrated';
import { usePageMeta } from '@/lib/usePageMeta';
import { PageHero } from '@/components/layout/PageHero';
import { Extras } from '@/sections/industries/Extras';
import { ShopTypeGrid } from '@/sections/industries/ShopTypeGrid';
import { ToolExplorer } from '@/sections/industries/ToolExplorer';

export default function Industries() {
  // ?shop=footwear picks a shop type; ?tool=pharmacy picks a tool directly.
  // The built HTML has no shop picked; the link's choice applies once the
  // page has hydrated, so the first render matches it.
  const [params, setParams] = useSearchParams();
  const hydrated = useHydrated();
  const shop = hydrated ? shopTypes.find((s) => s.slug === params.get('shop')) : undefined;
  const toolParam = hydrated ? params.get('tool') : null;
  const tool = toolOrder.includes(toolParam) ? toolParam : shop?.tool || toolOrder[0];

  const update = (next) => setParams(next, { replace: true, preventScrollReset: true });

  usePageMeta('/industries');

  return (
    <>
      <PageHero
        eyebrow="Industries"
        title="One POS for every kind of shop"
        lead="Grocery, clothing, pharmacy, electronics and more. Pick your shop type to see how MintoBill handles the work at your counter."
      />
      <ShopTypeGrid shop={shop?.slug} onPick={(slug) => update({ shop: slug })} />
      <ToolExplorer tool={tool} onSelect={(id) => update(shop ? { shop: shop.slug, tool: id } : { tool: id })} />
      <Extras />
    </>
  );
}
