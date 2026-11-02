# Symfony 7

## Project layout (Flex)

```
src/
├── Controller/
├── Entity/
├── Repository/
├── Service/
├── Command/
├── EventSubscriber/
└── Kernel.php
config/
├── services.yaml
├── routes.yaml
└── packages/
templates/
├── base.html.twig
└── ...
```

## Controller

```php
<?php

declare(strict_types=1);

namespace App\Controller;

use App\Entity\Product;
use App\Repository\ProductRepository;
use Symfony\Bundle\FrameworkBundle\Controller\AbstractController;
use Symfony\Component\HttpFoundation\JsonResponse;
use Symfony\Component\HttpFoundation\Request;
use Symfony\Component\HttpKernel\Attribute\MapRequestPayload;
use Symfony\Component\Routing\Attribute\Route;
use App\Dto\CreateProductDto;

final class ProductController extends AbstractController
{
    public function __construct(
        private readonly ProductRepository $repo,
    ) {}

    #[Route('/api/products/{id}', methods: ['GET'])]
    public function show(Product $product): JsonResponse
    {
        return $this->json($product, 200, [], ['groups' => 'product:read']);
    }

    #[Route('/api/products', methods: ['POST'])]
    public function create(#[MapRequestPayload] CreateProductDto $dto): JsonResponse
    {
        $product = $this->repo->create($dto);
        return $this->json($product, 201, [], ['groups' => 'product:read']);
    }
}
```

`#[MapRequestPayload]` (Symfony 7) auto-validates and deserializes the request body into the DTO. No manual `json_decode` + `Validator`.

## Dependency injection — autowiring

```php
// config/services.yaml
services:
    _defaults:
        autowire: true
        autoconfigure: true

    App\:
        resource: '../src/'
        exclude: '../src/{DependencyInjection,Entity,Kernel.php}'

    App\Repository\ProductRepositoryInterface: '@App\Repository\ProductRepository'
```

Constructor-injected classes are autowired by type. Bind interfaces to concrete classes in `services.yaml`.

## Entity + repository (Doctrine ORM)

```php
<?php

namespace App\Entity;

use Doctrine\ORM\Mapping as ORM;
use Symfony\Component\Serializer\Annotation\Groups;

#[ORM\Entity(repositoryClass: ProductRepository::class)]
#[ORM\Table(name: 'products')]
final class Product
{
    #[ORM\Id]
    #[ORM\GeneratedValue]
    #[ORM\Column]
    private ?int $id = null;

    #[ORM\Column(length: 200)]
    #[Groups(['product:read'])]
    private string $name;

    #[ORM\Column(precision: 10, scale: 2)]
    #[Groups(['product:read'])]
    private string $price;

    public function __construct(string $name, string $price)
    {
        $this->name = $name;
        $this->price = $price;
    }

    public function getId(): ?int { return $this->id; }
    public function getName(): string { return $this->name; }
    public function getPrice(): string { return $this->price; }
}
```

```php
<?php

namespace App\Repository;

use App\Entity\Product;
use Doctrine\Bundle\DoctrineBundle\Repository\ServiceEntityRepository;
use Doctrine\Persistence\ManagerRegistry;

/**
 * @extends ServiceEntityRepository<Product>
 */
final class ProductRepository extends ServiceEntityRepository implements ProductRepositoryInterface
{
    public function __construct(ManagerRegistry $registry)
    {
        parent::__construct($registry, Product::class);
    }

    public function create(CreateProductDto $dto): Product
    {
        $p = new Product($dto->name, $dto->price);
        $this->getEntityManager()->persist($p);
        $this->getEntityManager()->flush();
        return $p;
    }
}
```

## Messenger — async jobs

```php
<?php

namespace App\Message;

final class SendWelcomeEmail
{
    public function __construct(public readonly int $userId) {}
}

// Handler
final class SendWelcomeEmailHandler
{
    public function __construct(private readonly UserRepository $users) {}

    public function __invoke(SendWelcomeEmail $msg): void
    {
        $user = $this->users->find($msg->userId);
        // ... send email
    }
}

// Dispatch
$bus->dispatch(new SendWelcomeEmail($user->getId()));
```

```yaml
# config/packages/messenger.yaml
framework:
    messenger:
        transports:
            async: '%env(MESSENGER_TRANSPORT_DSN)%'
        routing:
            'App\Message\*': async
```

Run: `php bin/console messenger:consume async`.

## Console command

```php
<?php

namespace App\Command;

use Symfony\Component\Console\Attribute\AsCommand;
use Symfony\Component\Console\Command\Command;
use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputInterface;
use Symfony\Component\Console\Output\OutputInterface;

#[AsCommand(name: 'app:send-welcome', description: 'Send welcome emails')]
final class SendWelcomeCommand extends Command
{
    protected function configure(): void
    {
        $this->addArgument('userId', InputArgument::REQUIRED, 'User ID');
    }

    protected function execute(InputInterface $input, OutputInterface $output): int
    {
        $userId = (int) $input->getArgument('userId');
        // ... do work
        $output->writeln('Done.');
        return Command::SUCCESS;
    }
}
```

## Voters — fine-grained authorization

```php
<?php

namespace App\Security;

use App\Entity\Product;
use App\Entity\User;
use Symfony\Component\Security\Core\Authentication\Token\TokenInterface;
use Symfony\Component\Security\Core\Authorization\Voter\Voter;

final class ProductVoter extends Voter
{
    protected function supports(string $attribute, mixed $subject): bool
    {
        return in_array($attribute, ['edit', 'delete']) && $subject instanceof Product;
    }

    protected function voteOnAttribute(string $attribute, mixed $subject, TokenInterface $token): bool
    {
        $user = $token->getUser();
        if (!$user instanceof User) return false;
        return $subject->getOwnerId() === $user->getId();
    }
}

// Usage in controller:
#[Route('/products/{id}', methods: ['DELETE'])]
public function delete(Product $product): Response
{
    $this->denyAccessUnlessGranted('delete', $product);
    // ...
}
```

## Common pitfalls

- `EntityManager->flush()` per entity — batch flushes; one `flush()` at the end of a transaction
- Service container `dump()` in production — disable in `prod` env
- Controller extending `AbstractController` but using `get()`/`container` — use constructor DI
- Forgetting `groups` on serializer attributes — leaks all fields; configure groups per context
- Mixing Doctrine with Eloquent — pick one per app
- `messenger:consume` without supervisor — dies on memory leak; supervise in production
- Doctrine proxies lazy-loaded across requests (Octane) — detach/reattach or reload
