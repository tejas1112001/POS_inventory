import { usePageMeta } from '@/lib/usePageMeta';
import { Bento } from '@/sections/home/Bento';
import { HomeHero } from '@/sections/home/HomeHero';
import { HowItWorks } from '@/sections/home/HowItWorks';
import { IndustriesTeaser } from '@/sections/home/IndustriesTeaser';
import { TrustStrip } from '@/sections/home/TrustStrip';

export default function Home() {
  usePageMeta('/');
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
