<?php
declare(strict_types=1);

namespace VirelliumK9\Ledger\Entity;

use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
final class PrismShard
{
    #[ORM\Id]
    #[ORM\GeneratedValue]
    #[ORM\Column]
    private ?int $id = null;

    #[ORM\ManyToOne(inversedBy: 'shards')]
    #[ORM\JoinColumn(nullable: false)]
    private ?PrismVault $vault = null;

    public function vault(): ?PrismVault
    {
        return $this->vault;
    }
}
