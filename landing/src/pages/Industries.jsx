import { useSearchParams } from 'react-router-dom';
import { shopTypes, toolOrder } from '@/content/site';
import { usePageMeta } from '@/lib/usePageMeta';
import { PageHero } from '@/components/layout/PageHero';
import { Extras } from '@/sections/industries/Extras';
import { ShopTypeGrid } from '@/sections/industries/ShopTypeGrid';
import { ToolExplorer } from '@/sections/industries/ToolExplorer';

export default function Industries() {
  // ?shop=footwear picks a shop type; ?tool=pharmacy picks a tool directly.
  const [params, setParams] = useSearchParams();
  const shop = shopTypes.find((s) => s.slug === params.get('shop'));
  const toolParam = params.get('tool');
  const tool = toolOrder.includes(toolParam) ? toolParam : shop?.tool || toolOrder[0];

  const update = (next) => setParams(next, { replace: true, preventScrollReset: true });

  usePageMeta(
    'Industries | MintoBill',
    'Clothing, footwear, pharmacy, grocery, electronics and more. Switch on the MintoBill tools your shop needs.',
  );

  return (
    <>
      <PageHero
        eyebrow="Industries"
        title="One POS for every kind of shop"
        lead="Grocery, fashion, pharmacy, electronics and more. If you sell from a counter, MintoBill fits."
      />
      <ShopTypeGrid shop={shop?.slug} onPick={(slug) => update({ shop: slug })} />
      <ToolExplorer tool={tool} onSelect={(id) => update(shop ? { shop: shop.slug, tool: id } : { tool: id })} />
      <Extras />
    </>
  );
}
