import { usePageMeta } from '@/lib/usePageMeta';
import { Bento } from '@/sections/home/Bento';
import { HomeHero } from '@/sections/home/HomeHero';
import { HowItWorks } from '@/sections/home/HowItWorks';
import { IndustriesTeaser } from '@/sections/home/IndustriesTeaser';
import { TrustStrip } from '@/sections/home/TrustStrip';

export default function Home() {
  usePageMeta(
    'BillPiko: billing, stock and GST in one place',
    'BillPiko is a simple, fast point of sale for Indian shops. Make GST bills, keep stock correct and see your sales, all from your browser.',
  );
  return (
    <>
      <HomeHero />
      <TrustStrip />
      <Bento />
      <HowItWorks />
      <IndustriesTeaser />
      <div className="h-24" aria-hidden="true" />
    </>
  );
}
