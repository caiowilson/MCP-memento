<?php
declare(strict_types=1);

namespace App\Message\Cloudberry;

use Symfony\Component\DependencyInjection\Attribute\Autowire;

final class SpoolDrainer
{
    public function __construct(
        #[Autowire('%cloudberry.spool_batch_size%')]
        private int $batchSize,
    ) {
    }

    /**
     * @param list<string> $pending
     * @return list<string>
     */
    public function nextBatch(array $pending): array
    {
        return array_slice($pending, 0, $this->batchSize);
    }
}
