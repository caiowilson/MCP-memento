<?php

declare(strict_types=1);

namespace App\Entity;

use Doctrine\Common\Collections\Collection;
use Doctrine\ORM\Mapping as ORM;

final class Convoy
{
    /** @var Collection<int, ConvoyCheckpoint> */
    #[ORM\OneToMany(mappedBy: 'convoy', targetEntity: ConvoyCheckpoint::class, cascade: ['persist'], orphanRemoval: true)]
    private Collection $checkpoints;
}
